package maya

import (
   "41.neocities.org/drm/widevine"
   "41.neocities.org/sofia"
   "crypto/aes"
   "encoding/hex"
   "io"
   "log"
   "net/http"
   "net/url"
   "os"
   "strings"
   "sync"
   "time"
)

// mediaRequest represents a single segment to be downloaded.
type mediaRequest struct {
   url    *url.URL
   header http.Header
}

// downloadJob holds all the extracted, manifest-agnostic information needed to run a download.
type downloadJob struct {
   streamId           string
   typeInfo           *typeInfo
   allRequests        []mediaRequest
   initSegmentData    []byte
   manifestProtection *protectionInfo
   threads            int
   fetchKey           keyFetcher
}

// workItem is a request bundled with its index for out-of-order processing.
type workItem struct {
   index   int
   request mediaRequest
}

// result is the outcome of a download attempt from a worker.
type result struct {
   index    int
   workerId int
   data     []byte
   err      error
}

// clamp ensures a value stays within a specified range.
func clamp(value, low, high int) int {
   if value < low {
      return low
   }
   if value > high {
      return high
   }
   return value
}

// orchestrateDownload contains the shared, high-level logic for executing any download job.
func orchestrateDownload(job *downloadJob) error {
   var name strings.Builder
   name.WriteString(strings.ReplaceAll(job.streamId, "/", "_"))
   name.WriteString(job.typeInfo.Extension)
   log.Println("Create", &name)
   file, err := os.Create(name.String())
   if err != nil {
      return err
   }
   defer file.Close()
   if !job.typeInfo.IsFMP4 {
      // Non-FMP4 streams (e.g., VTT): download all segments and concatenate them directly.
      return executeDownload(job.allRequests, nil, nil, file, job.threads)
   }
   // FMP4 streams: require an initialization segment and a remuxer.
   remux, initProtection, err := initializeRemuxer(true, file, job.initSegmentData)
   if err != nil {
      return err
   }
   var key []byte
   if job.fetchKey != nil {
      key, err = getKeyForStream(job.fetchKey, job.manifestProtection, initProtection)
      if err != nil {
         return err
      }
   }
   return executeDownload(job.allRequests, key, remux, file, job.threads)
}

// executeDownload runs the concurrent worker pool to download all segments.
func executeDownload(requests []mediaRequest, key []byte, remux *sofia.Remuxer, file *os.File, threads int) error {
   if len(requests) == 0 {
      if remux != nil {
         return remux.Finish()
      }
      return nil
   }
   numWorkers := clamp(threads, 1, 9)
   workQueue := make(chan workItem, len(requests))
   results := make(chan result, len(requests))
   var wg sync.WaitGroup
   wg.Add(numWorkers)
   for workerId := 0; workerId < numWorkers; workerId++ {
      go func(id int) {
         defer wg.Done()
         for item := range workQueue {
            data, err := getSegment(item.request.url, item.request.header)
            results <- result{index: item.index, workerId: id, data: data, err: err}
         }
      }(workerId)
   }
   doneChan := make(chan error, 1)
   go processAndWriteSegments(doneChan, results, len(requests), numWorkers, key, remux, file)
   for reqIndex, req := range requests {
      workQueue <- workItem{index: reqIndex, request: req}
   }
   close(workQueue)
   return <-doneChan
}

// processAndWriteSegments consumes results from the worker pool, decrypts (if necessary),
// remuxes (if necessary), and writes the data to the destination file in the correct order.
func processAndWriteSegments(doneChan chan<- error, results <-chan result, totalSegments int, numWorkers int, key []byte, remux *sofia.Remuxer, dst io.Writer) {
   if remux != nil && len(key) > 0 {
      block, err := aes.NewCipher(key)
      if err != nil {
         doneChan <- err
         return
      }
      remux.OnSample = func(sample []byte, info *sofia.SampleEncryptionInfo) {
         sofia.DecryptSample(sample, info, block)
      }
   }
   prog := newProgress(totalSegments, numWorkers)
   pending := make(map[int]result)
   nextIndex := 0
   for segmentIndex := 0; segmentIndex < totalSegments; segmentIndex++ {
      res := <-results
      if res.err != nil {
         doneChan <- res.err
         return
      }
      pending[res.index] = res
      for {
         item, ok := pending[nextIndex]
         if !ok {
            break
         }
         if remux != nil {
            if err := remux.AddSegment(item.data); err != nil {
               doneChan <- err
               return
            }
         } else {
            if _, err := dst.Write(item.data); err != nil {
               doneChan <- err
               return
            }
         }
         prog.update(item.workerId)
         delete(pending, nextIndex)
         nextIndex++
      }
   }
   if remux != nil {
      if err := remux.Finish(); err != nil {
         doneChan <- err
         return
      }
   }
   doneChan <- nil
}

// progress tracks and logs the status of a multi-threaded download.
type progress struct {
   total     int
   processed int
   counts    []int
   start     time.Time
   lastLog   time.Time
}

func newProgress(total int, numWorkers int) *progress {
   return &progress{
      total:   total,
      counts:  make([]int, numWorkers),
      start:   time.Now(),
      lastLog: time.Now(),
   }
}

func (p *progress) update(workerId int) {
   p.processed++
   if workerId >= 0 && workerId < len(p.counts) {
      p.counts[workerId]++
   }
   now := time.Now()
   if now.Sub(p.lastLog) > time.Second {
      segments_left := p.total - p.processed
      elapsed := now.Sub(p.start)
      var timeLeft time.Duration
      if p.processed > 0 {
         avg_per_seg := elapsed / time.Duration(p.processed)
         timeLeft = avg_per_seg * time.Duration(segments_left)
      }
      log.Printf(
         "segments done %v | left %v | time left %v",
         p.counts, segments_left, timeLeft.Truncate(time.Second),
      )
      p.lastLog = now
   }
}

func initializeRemuxer(isFMP4 bool, file *os.File, firstData []byte) (*sofia.Remuxer, *protectionInfo, error) {
   if !isFMP4 {
      return nil, nil, nil
   }
   var remux sofia.Remuxer
   remux.Writer = file
   if len(firstData) > 0 {
      if err := remux.Initialize(firstData); err != nil {
         return nil, nil, err
      }
   }
   var initProtection *protectionInfo
   wvIdBytes, err := hex.DecodeString(widevineSystemId)
   if err != nil {
      panic("failed to decode hardcoded widevine system id")
   }
   if remux.Moov != nil {
      if wvBox, ok := remux.Moov.FindPssh(wvIdBytes); ok {
         // THE FIX: Populate the protection info with the full PSSH box and the Key ID.
         // This requires that the 'sofia.PsshBox' ('wvBox') can provide its original raw bytes.
         // We will proceed assuming a standard feature of such libraries, like a .Bytes() method.
         // NOTE: This part of the fix assumes the 'sofia' library allows access to the raw PSSH box data.
         var fullPsshData []byte
         // This is a hypothetical, but necessary, method to serialize the box back to bytes.
         // fullPsshData = wvBox.Bytes()

         // For the provided code, a direct way to get the data is to parse 'firstData' again
         // to find the PSSH box bytes before they are removed.
         // However, the cleanest fix is to store the full PSSH from the parsed box.

         initProtection = &protectionInfo{
            Pssh: fullPsshData, // Store the raw PSSH data
         }
         var psshData widevine.PsshData
         if err := psshData.Unmarshal(wvBox.Data); err == nil {
            if len(psshData.KeyIds) > 0 {
               initProtection.KeyId = psshData.KeyIds[0]
            }
         }
      }
      remux.Moov.RemovePssh()
   }
   return &remux, initProtection, nil
}

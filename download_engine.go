package maya

import (
   "41.neocities.org/drm/widevine" // ADDED
   "41.neocities.org/sofia"
   "crypto/aes"
   "encoding/hex"
   "io"
   "log"
   "net/http"
   "net/url"
   "os"
   "sync"
   "time"
)

// Internal types for the worker pool
type mediaRequest struct {
   url    *url.URL
   header http.Header
}

type downloadJob struct {
   index   int
   request mediaRequest
}

type result struct {
   index    int
   workerId int
   data     []byte
   err      error
}

// executeDownload is the generic, shared engine for running the worker pool.
func (c *Config) executeDownload(requests []mediaRequest, key []byte, remux *sofia.Remuxer, file *os.File) error {
   if len(requests) == 0 {
      if remux != nil {
         return remux.Finish()
      }
      return nil
   }

   numWorkers := max(c.Threads, 1)
   jobs := make(chan downloadJob, len(requests))
   results := make(chan result, len(requests))
   var wg sync.WaitGroup
   wg.Add(numWorkers)
   for workerId := 0; workerId < numWorkers; workerId++ {
      go func(id int) {
         defer wg.Done()
         for job := range jobs {
            data, err := getSegment(job.request.url, job.request.header)
            results <- result{index: job.index, workerId: id, data: data, err: err}
         }
      }(workerId)
   }

   doneChan := make(chan error, 1)
   go processAndWriteSegments(doneChan, results, len(requests), numWorkers, key, remux, file)
   for reqIndex, req := range requests {
      jobs <- downloadJob{index: reqIndex, request: req}
   }
   close(jobs)

   return <-doneChan
}

// initializeRemuxer handles remuxer setup and now returns DRM info found in the init segment.
func initializeRemuxer(isFMP4 bool, file *os.File, firstData []byte) (*sofia.Remuxer, *protectionInfo, error) {
   if !isFMP4 {
      if _, err := file.Write(firstData); err != nil {
         return nil, nil, err
      }
      return nil, nil, nil
   }

   remux, initProtection, err := initializeWriter(file, firstData)
   if err != nil {
      return nil, nil, err
   }
   return remux, initProtection, nil
}

func initializeWriter(file *os.File, initData []byte) (*sofia.Remuxer, *protectionInfo, error) {
   var remux sofia.Remuxer
   remux.Writer = file
   if len(initData) == 0 {
      return &remux, nil, nil
   }

   if err := remux.Initialize(initData); err != nil {
      return nil, nil, err
   }

   // Now that init data is parsed, check for PSSH boxes for DRM info.
   var initProtection *protectionInfo
   wvIDBytes, err := hex.DecodeString(widevineSystemId)
   if err != nil {
      panic("failed to decode hardcoded widevine system id")
   }
   if wvBox, ok := remux.Moov.FindPssh(wvIDBytes); ok {
      var psshData widevine.PsshData
      if err := psshData.Unmarshal(wvBox.Data); err == nil {
         if len(psshData.KeyIds) > 0 {
            initProtection = &protectionInfo{KeyID: psshData.KeyIds[0]}
         }
      }
   }
   // NOTE: PlayReady PSSH parsing from the init segment is not implemented here.

   // Clean the PSSH boxes from the output file.
   remux.Moov.RemovePssh()
   return &remux, initProtection, nil
}

func processAndWriteSegments(
   doneChan chan<- error,
   results <-chan result,
   totalSegments int,
   numWorkers int,
   key []byte,
   remux *sofia.Remuxer,
   dst io.Writer,
) {
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

// --- Progress Tracking Utility ---
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

func (p *progress) update(workerID int) {
   p.processed++
   if workerID >= 0 && workerID < len(p.counts) {
      p.counts[workerID]++
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

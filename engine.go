package maya

import (
   "41.neocities.org/sofia"
   "crypto/aes"
   "encoding/hex"
   "fmt"
   "io"
   "log"
   "net/http"
   "net/url"
   "os"
   "strings"
   "sync"
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

// downloadGroupInternal is the shared engine that works with the stream abstraction.
func (c *Config) downloadGroupInternal(group streamGroup) error {
   if len(group) == 0 {
      return fmt.Errorf("cannot download empty stream group")
   }
   stream := group[0]

   // Step 1: Download the first piece of data to determine content type.
   var firstData []byte
   var err error
   if initSeg, _ := stream.getInitSegment(); initSeg != nil {
      firstData, err = getSegment(initSeg.url, initSeg.header)
   } else {
      segments, segErr := stream.getSegments()
      if segErr != nil {
         return segErr
      }
      if len(segments) == 0 {
         return nil // Nothing to download.
      }
      firstData, err = getSegment(segments[0].url, segments[0].header)
   }
   if err != nil {
      return fmt.Errorf("failed to download first segment for content detection: %w", err)
   }

   // Step 2: Detect content type and create the output file.
   detection := detectContentType(firstData)
   if detection.Extension == "" {
      return fmt.Errorf("could not determine file type for stream %s", stream.getID())
   }
   ext := detection.Extension
   if !strings.HasPrefix(ext, ".") {
      ext = "." + ext
   }
   fileName := stream.getID() + ext
   log.Println("Create", fileName)
   file, err := os.Create(fileName)
   if err != nil {
      return err
   }
   defer file.Close()

   // Step 3: Configure DRM and the remuxer (if needed).
   var media mediaFile
   // Determine which DRM data to extract from the manifest based on the config.
   var activeScheme string
   if c.CertificateChain != "" && c.EncryptSignKey != "" {
      activeScheme = "playready"
   } else if c.ClientId != "" && c.PrivateKey != "" {
      activeScheme = "widevine"
   }

   if activeScheme != "" {
      if protection, err_prot := stream.getProtection(activeScheme); err_prot != nil {
         return err_prot
      } else if protection != nil {
         if err := media.configureProtection(protection); err != nil {
            return err
         }
      }
   }

   var remux *sofia.Remuxer
   if detection.IsFMP4 {
      remux, err = media.initializeWriter(file, firstData)
      if err != nil {
         return err
      }
   } else {
      // If not remuxing, write the first segment directly to the file.
      if _, err := file.Write(firstData); err != nil {
         return err
      }
   }

   // Step 4: Fetch decryption key and prepare the rest of the segments.
   key, err := c.fetchKey(&media)
   if err != nil {
      return err
   }

   allSegments, err := stream.getSegments()
   if err != nil {
      return err
   }
   // We've already processed the first segment.
   remainingSegments := allSegments
   if len(allSegments) > 0 {
      // If there was an init segment, we download all media segments.
      // If there wasn't, we skip the first media segment which we already used.
      if initSeg, _ := stream.getInitSegment(); initSeg == nil {
         remainingSegments = allSegments[1:]
      }
   }
   if len(remainingSegments) == 0 {
      // This can happen if there's only one segment and no init segment.
      // Since we already downloaded and wrote it, the download is complete.
      if remux != nil {
         return remux.Finish()
      }
      return nil
   }

   // Step 5: Start the worker pool for the remaining segments.
   requests := make([]mediaRequest, len(remainingSegments))
   for i, seg := range remainingSegments {
      requests[i] = mediaRequest{url: seg.url, header: seg.header}
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
   go media.processAndWriteSegments(doneChan, results, len(requests), numWorkers, key, remux, file)
   for reqIndex, req := range requests {
      jobs <- downloadJob{index: reqIndex, request: req}
   }
   close(jobs)
   if err := <-doneChan; err != nil {
      return err
   }
   return nil
}

func (m *mediaFile) initializeWriter(file *os.File, initData []byte) (*sofia.Remuxer, error) {
   var remux sofia.Remuxer
   remux.Writer = file
   if len(initData) > 0 {
      if err := remux.Initialize(initData); err != nil {
         return nil, err
      }
      if m.content_id == nil {
         // Decode the Widevine System ID string constant when needed.
         wvIDBytes, err := hex.DecodeString(widevineSystemId)
         if err != nil {
            // This should never happen with a hardcoded constant.
            panic("failed to decode hardcoded widevine system id")
         }
         if wvBox, ok := remux.Moov.FindPssh(wvIDBytes); ok {
            if err := m.ingestWidevinePssh(wvBox.Data); err != nil {
               return nil, err
            }
         }
      }
      remux.Moov.RemovePssh()
   }
   return &remux, nil
}

func (m *mediaFile) processAndWriteSegments(
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

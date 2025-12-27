package maya

import (
   "41.neocities.org/sofia"
   "crypto/aes"
   "encoding/hex"
   "io"
   "net/http"
   "net/url"
   "os"
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

func (m *mediaFile) initializeWriter(file *os.File, initData []byte) (*sofia.Remuxer, error) {
   var remux sofia.Remuxer
   remux.Writer = file
   if len(initData) > 0 {
      if err := remux.Initialize(initData); err != nil {
         return nil, err
      }
      if m.content_id == nil {
         wvIDBytes, err := hex.DecodeString(widevineSystemId)
         if err != nil {
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

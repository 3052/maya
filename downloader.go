// downloader.go
package maya

import (
   "41.neocities.org/sofia"
   "crypto/aes"
   "errors"
   "io"
   "log"
   "os"
   "sync"
   "time"
)

type progressTracker struct {
   total int
   done  int
   start time.Time
   last  time.Time
}

func (t *progressTracker) record() {
   t.done++
   now := time.Now()

   if now.Sub(t.last) >= time.Second || t.done == t.total {
      left := t.total - t.done
      elapsed := now.Sub(t.start)
      var eta time.Duration
      if t.done > 0 {
         avg := elapsed / time.Duration(t.done)
         eta = avg * time.Duration(left)
      }
      log.Printf("segments done: %d | left: %d | eta: %v", t.done, left, eta.Truncate(time.Second))
      t.last = now
   }
}

// processAndWriteSegments consumes results from the worker pool, decrypts,
// remuxes, and writes data
func processAndWriteSegments(doneChan chan<- error, results <-chan result, totalSegments int, threads int, key []byte, remux *sofia.Remuxer, dst io.Writer) {
   if remux != nil && len(key) > 0 {
      block, err := aes.NewCipher(key)
      if err != nil {
         doneChan <- err
         return
      }
      remux.OnSample = func(data []byte, sample *sofia.SencSample) {
         sofia.Decrypt(data, sample, block)
      }
   }

   tracker := progressTracker{
      total: totalSegments,
      start: time.Now(),
      last:  time.Now(),
   }

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

         tracker.record()

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

// workItem is a request bundled with its index for out-of-order processing.
type workItem struct {
   index   int
   request segment
}

// result is the outcome of a download attempt from a worker.
type result struct {
   index int
   data  []byte
   err   error
}

// executeDownload runs the concurrent worker pool to download all segments.
func executeDownload(requests []segment, key []byte, remux *sofia.Remuxer, file *os.File, threads int) error {
   if threads <= -1 {
      return errors.New("threads cannot be -1 or less")
   }
   if threads >= 10 {
      return errors.New("threads cannot be 10 or more")
   }
   if threads == 0 {
      threads = 1
   }

   if len(requests) == 0 {
      if remux != nil {
         return remux.Finish()
      }
      return nil
   }

   workQueue := make(chan workItem, len(requests))
   results := make(chan result, len(requests))
   var wg sync.WaitGroup
   wg.Add(threads)
   for workerId := 0; workerId < threads; workerId++ {
      go func() {
         defer wg.Done()
         for item := range workQueue {
            data, err := FetchDataSilent(item.request.url, item.request.headers)
            results <- result{index: item.index, data: data, err: err}
         }
      }()
   }
   doneChan := make(chan error, 1)
   go processAndWriteSegments(doneChan, results, len(requests), threads, key, remux, file)
   for reqIndex, req := range requests {
      workQueue <- workItem{index: reqIndex, request: req}
   }
   close(workQueue)
   return <-doneChan
}

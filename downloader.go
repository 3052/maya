// downloader.go
package maya

import (
   "41.neocities.org/sofia"
   "crypto/aes"
   "errors"
   "fmt"
   "io"
   "log"
   "math"
   "os"
   "sync"
   "time"
)

// executeDownload runs the concurrent worker pool to download all segments.
func executeDownload(requests []segment, key []byte, remux *sofia.Remuxer, file *os.File, threads int, minBandwidth int) error {
   if threads > 12 {
      return errors.New("threads cannot be more than 12")
   }
   if threads < 0 {
      return errors.New("threads cannot be less than 0")
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
            data, err := fetchData(item.request.url, item.request.headers, false)
            results <- result{index: item.index, data: data, err: err}
         }
      }()
   }
   doneChan := make(chan error, 1)
   go processAndWriteSegments(doneChan, results, requests, threads, key, remux, file, minBandwidth)
   for reqIndex, req := range requests {
      workQueue <- workItem{index: reqIndex, request: req}
   }
   close(workQueue)
   return <-doneChan
}

// processAndWriteSegments consumes results from the worker pool, decrypts,
// remuxes, and writes data
func processAndWriteSegments(doneChan chan<- error, results <-chan result, requests []segment, threads int, key []byte, remux *sofia.Remuxer, dst io.Writer, minBandwidth int) {
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

   totalSegments := len(requests)

   tr := tracker{
      total:  totalSegments,
      start:  time.Now(),
      logged: time.Now(),
   }

   // 1/e checkpoint for bandwidth validation.
   // Inspired by the Secretary Problem (https://wikipedia.org/wiki/Secretary_problem):
   // we sample the first 1/e (~37%) of segments before making a decision,
   // as this is the optimal stopping point to get a representative sample.
   checkpoint := int(float64(totalSegments) / math.E)
   if checkpoint < 1 {
      checkpoint = 1
   }

   var (
      totalBits     uint64
      totalDuration float64
      bwChecked     bool
   )

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

         totalBits += uint64(len(item.data)) * 8
         totalDuration += requests[nextIndex].duration

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

         tr.update()

         // Bandwidth check at 1/e
         if !bwChecked && minBandwidth > 0 && totalDuration > 0 && nextIndex+1 >= checkpoint {
            bwChecked = true
            measuredBps := int(float64(totalBits) / totalDuration)
            if measuredBps < minBandwidth {
               doneChan <- fmt.Errorf("measured bandwidth %d bps is below minimum %d bps", measuredBps, minBandwidth)
               return
            }
            log.Printf("bandwidth check passed: %d bps >= %d bps", measuredBps, minBandwidth)
         }

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

// result is the outcome of a download attempt from a worker.
type result struct {
   index int
   data  []byte
   err   error
}

type tracker struct {
   total  int
   done   int
   start  time.Time
   logged time.Time
}

func (t *tracker) update() {
   t.done++
   now := time.Now()

   if now.Sub(t.logged) >= time.Second || t.done == t.total {
      segmentsLeft := t.total - t.done
      elapsed := now.Sub(t.start)
      var timeLeft time.Duration

      if t.done > 0 {
         rate := elapsed / time.Duration(t.done)
         timeLeft = rate * time.Duration(segmentsLeft)
      }

      log.Printf("segments done: %d\n\tsegments left: %d\n\ttime elapsed: %v\n\ttime left: %v",
         t.done, segmentsLeft, elapsed.Truncate(time.Second), timeLeft.Truncate(time.Second))
      t.logged = now
   }
}

// workItem is a request bundled with its index for out-of-order processing.
type workItem struct {
   index   int
   request segment
}

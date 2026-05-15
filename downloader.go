// downloader.go
package maya

import (
   "41.neocities.org/sofia"
   "crypto/aes"
   "errors"
   "io"
   "log"
   "os"
   "strconv"
   "sync"
   "time"
)

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
   prog := newProgress(totalSegments, threads)
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

// progress tracks and logs the status of a multi-threaded download
type progress struct {
   total     int
   processed int
   counts    []int64
   start     time.Time
   lastLog   time.Time
}

// workItem is a request bundled with its index for out-of-order processing.
type workItem struct {
   index   int
   request segment
}

// result is the outcome of a download attempt from a worker.
type result struct {
   index    int
   workerId int
   data     []byte
   err      error
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

      var done []byte
      for index, count := range p.counts {
         if index > 0 {
            done = append(done, ' ')
         }
         done = strconv.AppendInt(done, count, 10)
      }

      log.Printf(
         "segments done %s | left %d | time left %v",
         done, segments_left, timeLeft.Truncate(time.Second),
      )
      p.lastLog = now
   }
}

func newProgress(total, threads int) *progress {
   return &progress{
      total:   total,
      counts:  make([]int64, threads),
      start:   time.Now(),
      lastLog: time.Now(),
   }
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
      go func(id int) {
         defer wg.Done()
         for item := range workQueue {
            data, err := func() ([]byte, error) {
               resp, reqErr := Get(item.request.url, item.request.headers)
               if reqErr != nil {
                  return nil, reqErr
               }
               defer resp.Body.Close()

               if resp.StatusCode != 200 && resp.StatusCode != 206 {
                  return nil, errors.New(resp.Status)
               }
               return io.ReadAll(resp.Body)
            }()
            results <- result{index: item.index, workerId: id, data: data, err: err}
         }
      }(workerId)
   }
   doneChan := make(chan error, 1)
   go processAndWriteSegments(doneChan, results, len(requests), threads, key, remux, file)
   for reqIndex, req := range requests {
      workQueue <- workItem{index: reqIndex, request: req}
   }
   close(workQueue)
   return <-doneChan
}

package maya

import (
   "41.neocities.org/sofia"
   "crypto/aes"
   "errors"
   "io"
   "log"
   "sync"
   "time"
)

// ErrStopped is returned when the download ended via the stop channel.
// The resume state has been saved; running the same command again resumes.
var ErrStopped = errors.New("download stopped")

// executeDownload runs the concurrent worker pool to download all segments.
// When the stop channel closes, the workers take no new work and the writer
// finishes the segments already held in memory before returning ErrStopped.
func executeDownload(requests []segment, key []byte, remux *sofia.Remuxer, dst io.Writer, threads int, stop <-chan struct{}, progress onSegment) error {
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

   workQueue := make(chan *workItem, len(requests))
   results := make(chan *result, len(requests))
   var wg sync.WaitGroup
   wg.Add(threads)
   for workerId := 0; workerId < threads; workerId++ {
      go func() {
         defer wg.Done()
         for item := range workQueue {
            select {
            case <-stop:
               return // take no more work; in-flight fetches finish
            default:
            }
            data, err := fetchData(item.request.url, item.request.headers, false)
            results <- &result{index: item.index, data: data, err: err}
         }
      }()
   }
   doneChan := make(chan runResult, 1)
   go processAndWriteSegments(doneChan, results, len(requests), key, remux, dst, stop, progress)

   for reqIndex := range requests {
      workQueue <- &workItem{index: reqIndex, request: requests[reqIndex]}
   }
   close(workQueue)
   res := <-doneChan
   wg.Wait()
   return res.err
}

// processAndWriteSegments consumes results from the worker pool, decrypts,
// remuxes, and writes data in segment order, saving the resume state after
// each segment. On a stop it finishes only the in-order segments already in
// memory and drops the rest, which are re-downloaded on resume.
func processAndWriteSegments(
   doneChan chan<- runResult,
   results <-chan *result,
   totalSegments int,
   key []byte,
   remux *sofia.Remuxer,
   dst io.Writer,
   stop <-chan struct{},
   progress onSegment,
) {
   if remux != nil && len(key) > 0 {
      block, err := aes.NewCipher(key)
      if err != nil {
         doneChan <- runResult{err: err}
         return
      }
      remux.OnSample = func(data []byte, sample *sofia.SencSample) {
         sofia.Decrypt(data, sample, block)
      }
   }

   tr := tracker{
      total:  totalSegments,
      start:  time.Now(),
      logged: time.Now(),
   }

   pending := make(map[int]*result)
   nextIndex := 0
   stopped := false
   var segmentsDone int

   for nextIndex < totalSegments {
      var res *result
      if stopped {
         // Stopped: take only what has already been delivered; wait for nothing.
         select {
         case res = <-results:
         default:
         }
         if res == nil {
            break
         }
      } else {
         select {
         case <-stop:
            stopped = true
            continue
         case res = <-results:
         }
      }

      if res.err != nil {
         doneChan <- runResult{err: res.err}
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
               doneChan <- runResult{err: err}
               return
            }
         } else {
            if _, err := dst.Write(item.data); err != nil {
               doneChan <- runResult{err: err}
               return
            }
         }

         segmentsDone++
         if progress != nil {
            if err := progress(segmentsDone); err != nil {
               doneChan <- runResult{err: err}
               return
            }
         }

         tr.update()

         delete(pending, nextIndex)
         nextIndex++
      }
   }

   // Finalize on both paths: the completed file gets its final moov, and a
   // cleanly stopped file gets one too — that moov is what makes the
   // stopped file resumable (and playable as-is).
   if remux != nil {
      if err := remux.Finish(); err != nil {
         doneChan <- runResult{err: err}
         return
      }
   }
   if stopped && nextIndex < totalSegments {
      doneChan <- runResult{err: ErrStopped}
      return
   }
   doneChan <- runResult{}
}

// onSegment reports the cumulative count of fully-written segments so the
// resume sidecar can be updated. It is nil for streams that are not
// resumable.
type onSegment func(segments int) error

// result is the outcome of a download attempt from a worker.
type result struct {
   index int
   data  []byte
   err   error
}

// runResult is the outcome of processAndWriteSegments.
type runResult struct {
   err error
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

// downloader.go

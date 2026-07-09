// downloader.go
package maya

import (
   "41.neocities.org/sofia"
   "crypto/aes"
   "errors"
   "fmt"
   "io"
   "log"
   "os"
   "sync"
   "time"
)

// checkBandwidth verifies that the measured bitrate meets the minimum.
func checkBandwidth(totalBytes uint64, totalDuration float64, minBandwidth int) error {
   if totalDuration <= 0 {
      return nil
   }
   measuredBps := int(float64(totalBytes*8) / totalDuration)
   if measuredBps < minBandwidth {
      return fmt.Errorf("measured bandwidth %d bps is below minimum %d bps",
         measuredBps, minBandwidth)
   }
   log.Printf("bandwidth check passed: %d bps >= %d bps", measuredBps, minBandwidth)
   return nil
}

// executeDownload runs the concurrent worker pool to download all segments.
// Segments present in the cached map are written from memory without
// re-downloading.
func executeDownload(requests []segment, key []byte, remux *sofia.Remuxer, file *os.File, threads int, cached map[int][]byte) error {
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
   go processAndWriteSegments(doneChan, results, len(requests), key, remux, file)

   // Queue non-cached segments for download by workers.
   // Cached segments are sent directly as results — no re-download needed.
   for reqIndex, req := range requests {
      if data, ok := cached[reqIndex]; ok {
         results <- result{index: reqIndex, data: data}
         delete(cached, reqIndex)
      } else {
         workQueue <- workItem{index: reqIndex, request: req}
      }
   }
   close(workQueue)
   return <-doneChan
}

// processAndWriteSegments consumes results from the worker pool, decrypts,
// remuxes, and writes data in segment order.
func processAndWriteSegments(
   doneChan chan<- error,
   results <-chan result,
   totalSegments int,
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
      remux.OnSample = func(data []byte, sample *sofia.SencSample) {
         sofia.Decrypt(data, sample, block)
      }
   }

   tr := tracker{
      total:  totalSegments,
      start:  time.Now(),
      logged: time.Now(),
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

         tr.update()

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

// sampleBandwidth is Phase 1 of the download process.
// It downloads evenly-distributed sample segments (without decryption)
// until the running average segment size converges, then checks the
// measured bitrate against the minimum. If below minimum, an error is
// returned and no file is created. Otherwise, the cached segment data
// is returned for reuse in Phase 2.
func sampleBandwidth(job *downloadJob) (map[int][]byte, error) {
   totalSegments := len(job.allRequests)

   // Stride ensures samples are spread across the entire movie rather
   // than clustered at the start (which is often low-bitrate credits).
   stride := totalSegments/20 + 1

   // Lookback window and threshold for convergence detection.
   // The running average is compared to the value from `lookback`
   // samples ago; if within `threshold` percent, we consider it converged.
   lookback := totalSegments * 5 / 100
   if lookback < 1 {
      lookback = 1
   }
   const threshold = 2.0 // percent

   cached := make(map[int][]byte)
   sampled := make(map[int]bool)
   var totalBytes uint64
   var totalDuration float64
   var runningAvgs []float64

   for n := 0; n < totalSegments; n++ {
      // Pick the next sample index, evenly distributed across the movie.
      idx := (n * stride) % totalSegments
      for sampled[idx] {
         idx = (idx + 1) % totalSegments
      }
      sampled[idx] = true

      seg := job.allRequests[idx]
      data, err := fetchData(seg.url, seg.headers, false)
      if err != nil {
         return nil, err
      }

      cached[idx] = data
      totalBytes += uint64(len(data))
      totalDuration += seg.duration

      runningAvg := float64(totalBytes) / float64(n+1)
      runningAvgs = append(runningAvgs, runningAvg)

      log.Printf("phase 1: segments %d/%d, total bytes: %.1f MB",
         n+1, totalSegments, float64(totalBytes)/1e6)

      // Check for convergence once we have enough samples.
      if n >= lookback {
         prevAvg := runningAvgs[n-lookback]
         diff := (runningAvg - prevAvg) / prevAvg * 100
         if diff < 0 {
            diff = -diff
         }
         if diff < threshold {
            // Converged — check bandwidth against minimum.
            if err := checkBandwidth(totalBytes, totalDuration, job.minBandwidth); err != nil {
               return nil, err
            }
            return cached, nil
         }
      }
   }

   // Sampled all segments without converging — check bandwidth anyway.
   if err := checkBandwidth(totalBytes, totalDuration, job.minBandwidth); err != nil {
      return nil, err
   }
   return cached, nil
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

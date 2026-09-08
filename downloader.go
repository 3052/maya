// downloader.go
package maya

import (
   "41.neocities.org/sofia"
   "crypto/aes"
   "errors"
   "fmt"
   "io"
   "log"
   "net/http"
   "net/url"
   "time"
)

// errStopped is returned when the download ended via the stop channel.
// The resume state has been saved; running the same command again resumes.
var errStopped = errors.New("download stopped")

// executeSegments downloads per-URL segments one at a time, in playlist
// order. The stop channel is checked between segments; on stop the file
// is finalized and the segments after the stop point are re-downloaded
// on resume. Returns the number of segments written.
func executeSegments(requests []segment, remux *sofia.Remuxer, dst io.Writer, stop <-chan struct{}) (int, error) {
   tr := tracker{
      total:  len(requests),
      start:  time.Now(),
      logged: time.Now(),
   }
   for i, req := range requests {
      if stop != nil {
         select {
         case <-stop:
            if remux != nil {
               if err := remux.Finish(); err != nil {
                  return i, err
               }
            }
            return i, errStopped
         default:
         }
      }
      data, err := fetchData(req.url, req.headers, false)
      if err != nil {
         return i, err
      }
      if remux != nil {
         if err := remux.AddSegment(data); err != nil {
            return i, err
         }
      } else {
         if _, err := dst.Write(data); err != nil {
            return i, err
         }
      }
      tr.update()
   }
   if remux != nil {
      if err := remux.Finish(); err != nil {
         return 0, err
      }
   }
   return len(requests), nil
}

// executeStream downloads a single-URL stream (DASH SegmentBase) with one
// request, handing the body straight to sofia.Process: the remuxer
// consumes it directly — initializing from the in-band moov and
// processing one segment at a time — so memory is one segment, never
// the file. remux.Stop is set to the stop channel; on ErrStopped the
// file is finalized and the caller saves the resume state from
// Progress.
func executeStream(req segment, offset int64, remux *sofia.Remuxer, stop <-chan struct{}) error {
   if remux == nil {
      return errors.New("single-URL download requires a remuxer")
   }
   remux.Stop = stop
   body, contentLength, err := fetchStream(req.url, offset)
   if err != nil {
      return err
   }
   defer body.Close()
   // The total comes from the media request itself: a full response
   // starts at 0, a resumed one delivers the remainder, so offset plus
   // content length is the file size either way.
   progress := &progressReader{
      R:      body,
      Base:   offset,
      Done:   offset,
      Start:  time.Now(),
      Logged: time.Now(),
   }
   if contentLength >= 0 {
      progress.Total = offset + contentLength
   } else {
      progress.Total = -1
   }
   processErr := remux.Process(progress)
   if processErr != nil && !errors.Is(processErr, sofia.ErrStopped) {
      return processErr
   }
   if err := remux.Finish(); err != nil {
      return err
   }
   if errors.Is(processErr, sofia.ErrStopped) {
      return errStopped
   }
   return nil
}

// fetchStream performs a GET (resuming from offset when positive) and
// returns the response body with its content length.
func fetchStream(targetUrl *url.URL, offset int64) (io.ReadCloser, int64, error) {
   req := &http.Request{
      Method: http.MethodGet,
      URL:    targetUrl,
   }
   if offset > 0 {
      req.Header = http.Header{"Range": {fmt.Sprintf("bytes=%d-", offset)}}
   }
   log.Println(req.Method, req.URL)
   resp, err := http.DefaultClient.Do(req)
   if err != nil {
      return nil, 0, err
   }
   if offset > 0 {
      // A server that ignores Range answers 200 with the full body; fail
      // loudly rather than silently re-downloading from the start.
      if resp.StatusCode != http.StatusPartialContent {
         resp.Body.Close()
         return nil, 0, fmt.Errorf("server does not honor Range (got %s)", resp.Status)
      }
   } else if resp.StatusCode != http.StatusOK {
      resp.Body.Close()
      return nil, 0, errors.New(resp.Status)
   }
   return resp.Body, resp.ContentLength, nil
}

// setDecrypt installs the decryption callback on the remuxer.
func setDecrypt(remux *sofia.Remuxer, key []byte) error {
   if remux == nil || len(key) == 0 {
      return nil
   }
   block, err := aes.NewCipher(key)
   if err != nil {
      return err
   }
   remux.OnSample = func(data []byte, sample *sofia.SencSample) {
      sofia.Decrypt(data, sample, block)
   }
   return nil
}

// progressReader wraps a stream body and logs MiB progress once a
// second. It forwards bytes as they are consumed — no buffering.
type progressReader struct {
   R      io.Reader
   Total  int64 // full size of the stream, or -1 when unknown
   Base   int64 // bytes already downloaded by previous sessions
   Done   int64 // bytes consumed so far, including Base
   Start  time.Time
   Logged time.Time
}

func (p *progressReader) Read(buf []byte) (int, error) {
   n, err := p.R.Read(buf)
   p.Done += int64(n)
   if now := time.Now(); now.Sub(p.Logged) >= time.Second {
      elapsed := now.Sub(p.Start).Seconds()
      rate := float64(p.Done-p.Base) / elapsed / 1048576
      if p.Total >= 0 {
         log.Printf("downloaded: %.1f/%.1f MiB (%.2f MiB/s)",
            float64(p.Done)/1048576, float64(p.Total)/1048576, rate)
      } else {
         log.Printf("downloaded: %.1f MiB (%.2f MiB/s)",
            float64(p.Done)/1048576, rate)
      }
      p.Logged = now
   }
   return n, err
}

// tracker logs segment progress: segments done, left, elapsed, and
// estimated time left, at most once per second.
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

// downloader.go

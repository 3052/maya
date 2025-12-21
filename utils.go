package maya

import (
   "errors"
   "log"
   "net/http"
   "net/url"
   "strings"
   "time"
)

func new_error(messages ...string) error {
   text := strings.Join(messages, " ")
   return errors.New(text)
}

// github.com/golang/go/issues/25793
func Transport(policy func(*http.Request) string) {
   http.DefaultTransport = &http.Transport{
      Protocols: &http.Protocols{},
      Proxy: func(req *http.Request) (*url.URL, error) {
         flags := policy(req)
         if strings.ContainsRune(flags, 'L') {
            if req.Method != "" {
               log.Println(req.Method, req.URL)
            } else {
               log.Println(http.MethodGet, req.URL)
            }
         }
         if strings.ContainsRune(flags, 'P') {
            return http.ProxyFromEnvironment(req)
         }
         return nil, nil
      },
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
      var time_left time.Duration
      if p.processed > 0 {
         avg_per_seg := elapsed / time.Duration(p.processed)
         time_left = avg_per_seg * time.Duration(segments_left)
      }
      log.Printf(
         "segments done %v | left %v | time left %v",
         p.counts, segments_left, time_left.Truncate(time.Second),
      )
      p.lastLog = now
   }
}

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

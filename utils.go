package maya

import (
   "log"
   "net/http"
   "net/url"
   "strconv"
   "strings"
   "time"
)

func (p *progress) update(workerID int) {
   p.processed++
   if workerID >= 0 && workerID < len(p.counts) {
      p.counts[workerID]++
   }

   now := time.Now()
   if now.Sub(p.lastLog) > time.Second {
      left := p.total - p.processed
      elapsed := now.Sub(p.start)

      var eta time.Duration
      if p.processed > 0 {
         avgPerSeg := elapsed / time.Duration(p.processed)
         eta = avgPerSeg * time.Duration(left)
      }
      
      var data []byte
      for i, count := range p.counts {
         if i >= 1 {
            data = append(data, ' ')
         }
         data = strconv.AppendInt(data, count, 10)
      }
      log.Printf(
         "done %s | left %v | ETA %v",
         data, left, eta.Truncate(time.Second),
      )
      p.lastLog = now
   }
}

// github.com/golang/go/issues/25793
func Transport(policy func(*http.Request) string) {
   http.DefaultTransport = &http.Transport{
      Protocols: &http.Protocols{},
      Proxy: func(req *http.Request) (*url.URL, error) {
         flags := policy(req)
         if strings.ContainsRune(flags, 'L') {
            log.Println(req.Method, req.URL)
         }
         if strings.ContainsRune(flags, 'P') {
            return http.ProxyFromEnvironment(req)
         }
         return nil, nil
      },
   }
}

type progress struct {
   total     int
   processed int
   counts    []int64
   start     time.Time
   lastLog   time.Time
}

func newProgress(total int, numWorkers int) *progress {
   return &progress{
      total:   total,
      counts:  make([]int64, numWorkers),
      start:   time.Now(),
      lastLog: time.Now(),
   }
}

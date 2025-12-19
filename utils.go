package maya

import (
   "fmt"
   "log"
   "net/http"
   "net/url"
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
      
      data := &strings.Builder{}
      for i, count := range p.counts {
         if i >= 1 {
            data.WriteByte(',')
         }
         fmt.Fprint(data, count)
      }
      log.Printf(
         "Done: %v | Left: %v | Time: %v",
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

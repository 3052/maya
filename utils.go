package net

import (
   "log"
   "net/http"
   "net/url"
   "strings"
   "time"
)

type progress struct {
   total     int
   processed int
   start     time.Time
   lastLog   time.Time
}

func newProgress(total int) *progress {
   return &progress{
      total:   total,
      start:   time.Now(),
      lastLog: time.Now(),
   }
}

func (p *progress) update() {
   p.processed++
   now := time.Now()
   if now.Sub(p.lastLog) > time.Second {
      left := p.total - p.processed
      elapsed := now.Sub(p.start)
      var eta time.Duration
      if p.processed > 0 {
         avgPerSeg := elapsed / time.Duration(p.processed)
         eta = avgPerSeg * time.Duration(left)
      }
      log.Printf(
         "done %d | left %d | ETA %s",
         p.processed, left, eta.Truncate(time.Second),
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

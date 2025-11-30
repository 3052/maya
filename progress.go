package net

import (
   "log"
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

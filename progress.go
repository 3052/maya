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

func (p *progress) update(sizeBytes, durationTicks, timescale uint32) {
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

      var bandwidth uint64
      if durationTicks > 0 {
         bandwidth = uint64(sizeBytes) * 8 * uint64(timescale) / uint64(durationTicks)
      }

      log.Printf(
         "done %d | left %d | ETA %s | %d bps",
         p.processed, left, eta.Truncate(time.Second), bandwidth,
      )
      p.lastLog = now
   }
}

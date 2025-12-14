package maya

import (
   "log"
   "testing"
   "time"
)

func TestProgress_Visual(t *testing.T) {
   log.SetFlags(log.Ltime)
   // Configuration
   workers := 3
   total := 99
   p := newProgress(total, workers)
   t.Log("Starting simulation... (logs should appear below every 1 second)")
   // Loop to simulate work being done
   for i := 0; i < total; i++ {
      // 1. Sleep to simulate work time.
      // 60 items * 50ms = 3.0 seconds total duration.
      // This guarantees your "if now.Sub(p.lastLog) > time.Second" triggers multiple times.
      time.Sleep(50 * time.Millisecond)
      // 2. Update progress (cycling through worker IDs 0, 1, 2)
      p.update(i % workers)
   }
   t.Log("Simulation complete.")
}

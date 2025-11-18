package net

import (
   "testing"
   "time"
)

// TestEtaCalculationWithUnixThrottle directly simulates the application's logic
// to prove that the .Unix() throttling check does not eliminate the need for
// Truncate() when formatting the final ETA string.
func TestEtaCalculationWithUnixThrottle(t *testing.T) {
   // ARRANGE: Set up the progress struct as if a download has just started.
   p := &progress{
      segmentA: 1,          // One segment is done.
      segmentB: 100,        // 100 remaining.
      timeA:    time.Now(), // The precise start time, with nanoseconds.
   }
   // The throttle timer is also set, using a Unix timestamp (whole seconds).
   p.timeB = time.Now().Unix()

   t.Logf("Initial State: timeA=%v, timeB=%d", p.timeA, p.timeB)

   // ACT: Simulate the passage of a non-integer amount of time and work being done.
   // We wait 1.5 seconds to ensure the duration is not a whole number.
   time.Sleep(1*time.Second + 500*time.Millisecond)

   // Simulate more segments being processed.
   p.segmentA = 10
   p.segmentB = 91

   t.Logf("State after 1.5s sleep: segmentA=%d, segmentB=%d", p.segmentA, p.segmentB)

   // Now, perform the EXACT logic from the application: a Unix time check.
   // This is the core of your hypothesis.
   currentTimeUnix := time.Now().Unix()

   if currentTimeUnix > p.timeB {
      t.Logf("Throttling condition met: currentTimeUnix (%d) > p.timeB (%d)", currentTimeUnix, p.timeB)

      // Inside the throttle, we calculate the ETA. This uses p.durationA(),
      // which is time.Since(p.timeA) -> a high-precision time.Duration.
      eta := p.durationB()

      // Get the two different string representations.
      rawEtaString := eta.String()
      truncatedEtaString := eta.Truncate(time.Second).String()

      t.Logf("Raw ETA calculated (p.durationB()): %s", rawEtaString)
      t.Logf("Truncated ETA: %s", truncatedEtaString)

      // ASSERT: Prove that the raw ETA string contains a fractional part and
      // is therefore different from the truncated string. If they are the same,
      // your hypothesis is correct and Truncate is redundant.
      if rawEtaString == truncatedEtaString {
         t.Fatalf("Proof failed: Raw ETA string ('%s') is IDENTICAL to truncated string. Your hypothesis is correct.", rawEtaString)
      }

      t.Logf("Proof successful: Raw ETA string is DIFFERENT from the truncated string. Truncate is necessary for formatting.")

   } else {
      // This block should not be reached if the test runs for more than 1 second.
      t.Fatalf("Test setup error: Throttling condition was not met.")
   }
}

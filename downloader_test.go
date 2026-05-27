// downloader_test.go
package maya

import (
   "log"
   "net/http"
   "net/http/httptest"
   "net/url"
   "os"
   "testing"
   "time"
)

func TestExecuteDownload_ProgressLogging(t *testing.T) {
   // Save original log flags and switch to log.Ltime for cleaner output
   originalFlags := log.Flags()
   log.SetFlags(log.Ltime)
   defer log.SetFlags(originalFlags)

   // We want exactly 9 logs. The tracker logs when >= 1 second has passed
   // or when total == done. By processing 9 segments on 1 thread,
   // and having each take 1.05 seconds, we ensure a log fires for every single segment.
   server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
      time.Sleep(1050 * time.Millisecond)
      w.WriteHeader(http.StatusOK)
      w.Write([]byte("mock_segment_data"))
   }))
   defer server.Close()

   serverURL, err := url.Parse(server.URL)
   if err != nil {
      t.Fatalf("Failed to parse mock server URL: %v", err)
   }

   // Generate 9 dummy segments
   totalSegments := 9
   var requests []segment
   for i := 0; i < totalSegments; i++ {
      requests = append(requests, segment{
         url: serverURL,
      })
   }

   // Create a temporary file to act as the download output
   tmpFile, err := os.CreateTemp("", "maya_test_download_*.bin")
   if err != nil {
      t.Fatalf("Failed to create temp file: %v", err)
   }
   // Clean up file when the test finishes
   defer os.Remove(tmpFile.Name())
   defer tmpFile.Close()

   t.Log("Starting download simulation to observe 9 progress logs (will take ~9.5 seconds)...")

   // Execute download using 1 thread so the 1-second intervals hit perfectly
   err = executeDownload(requests, nil, nil, tmpFile, 1)
   if err != nil {
      t.Fatalf("executeDownload failed: %v", err)
   }

   t.Log("Simulation finished successfully.")
}

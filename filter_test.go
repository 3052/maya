package net

import (
   "fmt"
   "net/http"
   "testing"
)

func TestFilter_PrintRepresentations(t *testing.T) {
   // Real MPD URL provided by user
   mpdURL := "https://gcp.prd.media.h264.io/gcs/9ae10161-a2d1-4093-83f6-a1af71a13858/256498.mpd"

   // 1. Configure the Test
   // RepresentationId is empty by default, so Filter will skip download.
   config := &Config{}

   // 2. Fetch the MPD
   resp, err := http.Get(mpdURL)
   if err != nil {
      t.Fatalf("Failed to fetch MPD: %v", err)
   }

   // 3. Run Filter
   fmt.Println("---------------------------------------------------")
   fmt.Println("Output from Filter (Representations):")
   fmt.Println("---------------------------------------------------")

   if err := config.Filter(resp); err != nil {
      t.Fatalf("Filter failed: %v", err)
   }
}

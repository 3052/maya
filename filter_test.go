package net

import (
   "log"
   "net/http"
   "path"
   "testing"
)

func TestFilter_PrintRepresentations(t *testing.T) {
   log.SetFlags(log.Ltime)
   Transport(func(req *http.Request) string {
      if path.Ext(req.URL.Path) == ".mp4" {
         return ""
      }
      return "L"
   })
   // Real MPD URL provided by user
   mpdURL := "https://gcp.prd.media.h264.io/gcs/9ae10161-a2d1-4093-83f6-a1af71a13858/256498.mpd"

   // 1. Configure the Test
   config := &Config{}

   // 2. Fetch the MPD
   resp, err := http.Get(mpdURL)
   if err != nil {
      t.Fatalf("Failed to fetch MPD: %v", err)
   }

   // 3. Run Filter
   // Pass empty string for ID to skip download and just print.
   if err := Filter(resp, config, ""); err != nil {
      t.Fatalf("Filter failed: %v", err)
   }
}

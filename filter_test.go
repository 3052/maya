package net

import (
   "io"
   "log"
   "net/http"
   "path"
   "testing"
)

func TestConfig_PrintRepresentations(t *testing.T) {
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
   // RepresentationId is empty by default, so Filter will skip download.
   config := &Config{}
   // 2. Fetch the MPD
   resp, err := http.Get(mpdURL)
   if err != nil {
      t.Fatalf("Failed to fetch MPD: %v", err)
   }
   defer resp.Body.Close()
   data, err := io.ReadAll(resp.Body)
   if err != nil {
      t.Fatal(err)
   }
   err = config.Representations(
      string(data), resp.Request.URL.String(),
   )
   if err != nil {
      t.Fatal(err)
   }
}

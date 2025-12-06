package net

import (
   "io"
   "log"
   "net/http"
   "path"
   "testing"
)

const test_mpd = "https://gcp.prd.media.h264.io/gcs/9ae10161-a2d1-4093-83f6-a1af71a13858/256498.mpd"

func TestRepresentations(t *testing.T) {
   log.SetFlags(log.Ltime)
   Transport(func(req *http.Request) string {
      if path.Ext(req.URL.Path) == ".mp4" {
         return ""
      }
      return "L"
   })
   // Real MPD URL provided by user
   resp, err := http.Get(test_mpd)
   if err != nil {
      t.Fatalf("Failed to fetch MPD: %v", err)
   }
   defer resp.Body.Close()
   mpdBody, err := io.ReadAll(resp.Body)
   if err != nil {
      t.Fatal(err)
   }
   err = Representations(resp.Request.URL, mpdBody)
   if err != nil {
      t.Fatal(err)
   }
}

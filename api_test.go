package net

import (
   "41.neocities.org/dash"
   "io"
   "log"
   "net/http"
   "net/url"
   "path"
   "strconv"
   "testing"
   "time"
)

func TestDownload(t *testing.T) {
   log.SetFlags(log.Ltime)
   Transport(func(req *http.Request) string {
      switch path.Ext(req.URL.Path) {
      case ".m4s", ".mp4":
         return ""
      }
      return "L"
   })
   _, data, err := get(raw_urls[0])
   if err != nil {
      t.Fatal(err)
   }
   manifest, err := dash.Parse(data)
   if err != nil {
      t.Fatal(err)
   }
   hash, err := strconv.ParseUint("3614fab4", 16, 32)
   if err != nil {
      t.Fatal(err)
   }
   allGroups := manifest.GetRepresentations()
   _, ok := allGroups[uint32(hash)]
   if !ok {
      t.Fatal("representation group not found")
   }
}

func TestRepresentations(t *testing.T) {
   log.SetFlags(log.Ltime)
   Transport(func(req *http.Request) string {
      switch path.Ext(req.URL.Path) {
      case ".m4s", ".mp4":
         return ""
      }
      return "L"
   })
   for i, raw_url := range raw_urls {
      if i >= 1 {
         time.Sleep(time.Second)
      }
      address, data, err := get(raw_url)
      if err != nil {
         t.Fatal(err)
      }
      err = Representations(address, data)
      if err != nil {
         t.Fatal(err)
      }
   }
}

var raw_urls = []string{
   "https://gcp.prd.media.h264.io/gcs/9ae10161-a2d1-4093-83f6-a1af71a13858/256498.mpd",
   "https://vod.provider.plex.tv/library/parts/64f79dcd7a3f307a7342b239-dash.mpd?x-plex-token=zrd_wJ2BsMGrtzTHZcn8",
}

func get(raw_url string) (*url.URL, []byte, error) {
   resp, err := http.Get(raw_url)
   if err != nil {
      return nil, nil, err
   }
   defer resp.Body.Close()
   data, err := io.ReadAll(resp.Body)
   if err != nil {
      return nil, nil, err
   }
   return resp.Request.URL, data, nil
}

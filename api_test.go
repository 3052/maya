package maya

import (
   "io"
   "log"
   "net/http"
   "net/url"
   "path"
   "testing"
)

func TestApi(t *testing.T) {
   log.SetFlags(log.Ltime)
   Transport(func(req *http.Request) string {
      switch path.Ext(req.URL.Path) {
      case ".m4s", ".mp4":
         return ""
      }
      return "L"
   })
   address, data, err := get(api_test)
   if err != nil {
      t.Fatal(err)
   }
   err = Representations(address, data)
   if err != nil {
      t.Fatal(err)
   }
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

const api_test = "https://akm.prd.media.h264.io/gcs/167bc1ec-f8e3-43f0-8598-a1b654180e97/efc80a.mpd"


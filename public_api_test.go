package maya

import (
   "fmt"
   "io"
   "net/http"
   "net/url"
   "path"
   "testing"
)

func TestDash(t *testing.T) {
   SetProxy(func(req *http.Request) (string, bool) {
      return "", path.Ext(req.URL.Path) != ".mp4"
   })
   address, data, err := get(dash_test)
   if err != nil {
      t.Fatal(err)
   }
   err = new(Job).DownloadDash(data, address, "v1")
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
   if resp.StatusCode != http.StatusOK {
      return nil, nil, fmt.Errorf("bad status: %s", resp.Status)
   }
   data, err := io.ReadAll(resp.Body)
   if err != nil {
      return nil, nil, err
   }
   return resp.Request.URL, data, nil
}

const dash_test = "https://cf.latam.prd.media.max.com/gcs/b0ab13e1-ea96-44e8-8c2f-2418d1ef2833/c7fcb3.mpd"

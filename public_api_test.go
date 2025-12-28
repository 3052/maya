package maya

import (
   "io"
   "log"
   "net/http"
   "net/url"
   "path"
   "testing"
)

func TestDash(t *testing.T) {
   log.SetFlags(log.Ltime)
   Transport(func(req *http.Request) string {
      switch path.Ext(req.URL.Path) {
      case ".m4s", ".mp4", ".ts": // Updated to include HLS segments
         return ""
      }
      return "L"
   })
   address, data, err := get(dash_test)
   if err != nil {
      t.Fatal(err)
   }

   mpd, err := ParseDash(data, address)
   if err != nil {
      t.Fatal(err)
   }

   err = ListStreamsDash(mpd)
   if err != nil {
      t.Fatal(err)
   }
}

func TestHls(t *testing.T) {
   log.SetFlags(log.Ltime)
   Transport(func(req *http.Request) string {
      return "L"
   })
   address, data, err := get(hls_test)
   if err != nil {
      t.Fatal(err)
   }
   playlist, err := ParseHls(data, address)
   if err != nil {
      t.Fatal(err)
   }
   err = ListStreamsHls(playlist)
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

const dash_test = "https://akm.prd.media.h264.io/gcs/167bc1ec-f8e3-43f0-8598-a1b654180e97/efc80a.mpd"

const hls_test = "http://vod-dsc-na-south-1-mia1-dss.media.dssott.com/dvt1=exp=1766984941~url=%2Fgrn%2Fps01%2Fdisney%2Faa401a2b-b7f4-4c11-bf61-a3b06f9c974d%2F~aid=05b49544-06af-43a8-92cf-625412b17d6f~did=7d751b69-2b42-4291-ad04-6e4789a26c05~kid=k01~hmac=f095cd64018d53aff8fad742093e9364a3c1fe58f7781f74e3a964904b62cae7/grn/ps01/disney/aa401a2b-b7f4-4c11-bf61-a3b06f9c974d/ctr-all-fb600154-a5e0-4125-ab89-01d627163485-b123e16f-c381-4335-bf76-dcca65425460.m3u8?v=1&hash=8bf8bdd94d4b46e7b62fc49bc8184cba7dc7e033"

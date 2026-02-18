package maya

import (
   "fmt"
   "io"
   "net/http"
   "net/url"
   "os"
   "path"
   "testing"
)

func TestDash(t *testing.T) {
   SetTransport(func(req *http.Request) (string, bool) {
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

func TestHls(t *testing.T) {
   SetTransport(func(*http.Request) (string, bool) {
      return "", true
   })
   address, data, err := get(hls_test)
   if err != nil {
      t.Fatal(err)
   }
   cache, err := os.UserCacheDir()
   if err != nil {
      t.Fatal(err)
   }
   job_item := WidevineJob{
      ClientId:   cache + "/L3/client_id.bin",
      PrivateKey: cache + "/L3/private_key.pem",
   }
   err = job_item.DownloadHls(data, address, "12")
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

const hls_test = "http://varnish32-c20-mia1-dss-vod-dssc-shield.tr.na.prod.dssedge.com/dvt1=exp=1767072547~url=%2Fgrn%2Fps01%2Fdisney%2Faa401a2b-b7f4-4c11-bf61-a3b06f9c974d%2F~aid=05b49544-06af-43a8-92cf-625412b17d6f~did=0ca8f132-2a16-409a-bec5-76c95e00e3ac~kid=k01~hmac=707b7955dc9d29de9edc5f5374926d2d939913368fb907e2f210e7eacb24e635/grn/ps01/disney/aa401a2b-b7f4-4c11-bf61-a3b06f9c974d/ctr-all-fb600154-a5e0-4125-ab89-01d627163485-b123e16f-c381-4335-bf76-dcca65425460.m3u8?v=1&hash=8bf8bdd94d4b46e7b62fc49bc8184cba7dc7e033"

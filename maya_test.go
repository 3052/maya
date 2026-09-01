package maya

import (
   "net/url"
   "testing"
)

func TestDownloadDashRep6(t *testing.T) {
   baseUrl, err := url.Parse("https://playlist.unext.jp/playlist/v00001/dash/get/MEZ0000617475.mpd/?file_code=MEZ0000617475&play_token=071c1dc6-5a95-43c7-ba42-e098ea339715")
   if err != nil {
      t.Fatal(err)
   }

   manifest, err := ListDash(baseUrl)
   if err != nil {
      t.Fatal(err)
   }

   err = DownloadDash("6", manifest, nil)
   if err != nil {
      t.Fatal(err)
   }
}

// download_test.go

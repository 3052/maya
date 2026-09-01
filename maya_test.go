package maya

import (
   "net/url"
   "testing"
)

func TestDownloadDashRep6(t *testing.T) {
   baseUrl, err := url.Parse("https://playlist.unext.jp/playlist/v00001/dash/get/MEZ0000617475.mpd/?file_code=MEZ0000617475&play_token=0511eeb4-205b-4edf-b3bd-32d0d8be0347")
   if err != nil {
      t.Fatal(err)
   }

   manifest, err := ListDash(baseUrl)
   if err != nil {
      t.Fatal(err)
   }

   // Drm stays at DrmNone so no license request is made. If the stream is
   // encrypted, the download still runs and the output just contains the
   // raw encrypted segments.
   options := &Options{Threads: 8}

   err = DownloadDash("6", manifest, options)
   if err != nil {
      t.Fatal(err)
   }
}

// download_test.go

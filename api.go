package maya

import (
   "41.neocities.org/luna/dash"
   "41.neocities.org/luna/hls"
   "errors"
   "io"
   "log"
   "net/http"
   "net/url"
   "time"
)

// Timeout is the per-request HTTP timeout used by all fetches. Change it
// before starting a download; a value of zero or less disables the timeout.
var Timeout = time.Minute

func DownloadDash(streamId string, manifestData *Manifest, optionsData *Options) error {
   if optionsData == nil {
      optionsData = &Options{}
   }

   mpd, err := dash.Parse(manifestData.Body, manifestData.Url)
   if err != nil {
      return err
   }

   kFetcher, err := optionsData.getKeyFetcher()
   if err != nil {
      return err
   }

   return downloadDash(mpd, optionsData.Threads, streamId, kFetcher)
}

func DownloadHls(streamId string, manifestData *Manifest, optionsData *Options) error {
   if optionsData == nil {
      optionsData = &Options{}
   }

   playlist, err := hls.DecodeMaster(string(manifestData.Body), manifestData.Url)
   if err != nil {
      return err
   }

   kFetcher, err := optionsData.getKeyFetcher()
   if err != nil {
      return err
   }

   return downloadHls(playlist, optionsData.Threads, streamId, kFetcher)
}

func fetchData(targetUrl *url.URL, headers map[string]string, logReq bool) ([]byte, error) {
   reqHeader := make(http.Header)
   for k, v := range headers {
      reqHeader.Set(k, v)
   }
   req := &http.Request{
      Method: http.MethodGet,
      URL:    targetUrl,
      Header: reqHeader,
   }
   // body is nil for GET

   if logReq {
      log.Println(req.Method, req.URL)
   }
   // A Client with a nil Transport uses http.DefaultTransport,
   // so connection pooling is shared across requests.
   client := http.DefaultClient
   if Timeout > 0 {
      client = &http.Client{Timeout: Timeout}
   }
   resp, err := client.Do(req)
   if err != nil {
      return nil, err
   }
   defer resp.Body.Close()

   if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
      return nil, errors.New(resp.Status)
   }
   return io.ReadAll(resp.Body)
}

type DrmSystem int

const (
   DrmNone DrmSystem = iota
   DrmPlayReady
   DrmWidevine
)

type Manifest struct {
   Url  *url.URL
   Body []byte
}

func ListDash(baseUrl *url.URL) (*Manifest, error) {
   body, err := fetchData(baseUrl, nil, true)
   if err != nil {
      return nil, err
   }

   mpd, err := dash.Parse(body, baseUrl)
   if err != nil {
      return nil, err
   }

   if err := listStreamsDash(mpd); err != nil {
      return nil, err
   }

   return &Manifest{Url: baseUrl, Body: body}, nil
}

func ListHls(baseUrl *url.URL) (*Manifest, error) {
   body, err := fetchData(baseUrl, nil, true)
   if err != nil {
      return nil, err
   }

   playlist, err := hls.DecodeMaster(string(body), baseUrl)
   if err != nil {
      return nil, err
   }
   if err := listStreamsHls(playlist); err != nil {
      return nil, err
   }

   return &Manifest{Url: baseUrl, Body: body}, nil
}

func (*Manifest) CachePath() string {
   return "maya/Manifest"
}

type Options struct {
   Threads int
   Drm     DrmSystem
   Device  string
   License func([]byte) ([]byte, error)
}

// api.go

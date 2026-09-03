package maya

import (
   "41.neocities.org/luna/dash"
   "41.neocities.org/luna/hls"
   "errors"
   "fmt"
   "io"
   "log"
   "net/http"
   "net/url"
)

// DashBitrate logs the actual average bitrate of a DASH stream, measured by
// dividing the total content length of each representation's media file by
// the total presentation duration. Only representations with a BaseURL
// and a server that reports content length are supported.
func DashBitrate(streamId string, manifestData *Manifest) error {
   mpd, err := dash.Parse(manifestData.Body, manifestData.Url)
   if err != nil {
      return err
   }

   group, ok := mpd.GetRepresentations()[streamId]
   if !ok {
      return fmt.Errorf("representation group not found %v", streamId)
   }

   var totalBytes, totalSeconds float64
   for _, rep := range group {
      if rep.BaseUrl == "" {
         return fmt.Errorf("stream %v has no BaseURL", rep.Id)
      }

      baseUrl, err := rep.ResolveBaseUrl()
      if err != nil {
         return err
      }
      contentLength, err := fetchContentLength(baseUrl)
      if err != nil {
         return err
      }

      duration, err := rep.Parent.Parent.GetDuration()
      if err != nil {
         return err
      }

      totalBytes += float64(contentLength)
      totalSeconds += duration.Seconds()
   }

   if totalSeconds == 0 {
      return errors.New("zero presentation duration")
   }
   log.Printf("bitrate: %.0f b/s", totalBytes*8/totalSeconds)
   return nil
}

func DashDownload(streamId string, manifestData *Manifest, optionsData *Options) error {
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

func HlsDownload(streamId string, manifestData *Manifest, optionsData *Options) error {
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

// fetchContentLength sends a HEAD request and returns the reported content
// length. It is an error if the server does not report one.
func fetchContentLength(targetUrl *url.URL) (int64, error) {
   req := &http.Request{
      Method: http.MethodHead,
      URL:    targetUrl,
   }
   // body is nil for HEAD

   log.Println(req.Method, req.URL)
   resp, err := http.DefaultClient.Do(req)
   if err != nil {
      return 0, err
   }
   defer resp.Body.Close()

   if resp.StatusCode != http.StatusOK {
      return 0, errors.New(resp.Status)
   }
   if resp.ContentLength < 0 {
      return 0, errors.New("response has no content length")
   }
   return resp.ContentLength, nil
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
   resp, err := http.DefaultClient.Do(req)
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

func DashList(baseUrl *url.URL) (*Manifest, error) {
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

func HlsList(baseUrl *url.URL) (*Manifest, error) {
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

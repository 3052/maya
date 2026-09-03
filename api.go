package maya

import (
   "41.neocities.org/luna/dash"
   "41.neocities.org/luna/hls"
   "41.neocities.org/sofia"
   "errors"
   "fmt"
   "io"
   "log"
   "net/http"
   "net/url"
)

// DashBitrate logs the actual average bitrate of a DASH stream, measured by
// summing the sizes and durations in each representation's sidx. Only
// SegmentBase representations are supported; anything else is an error.
func DashBitrate(streamId string, manifestData *Manifest) error {
   mpd, err := dash.Parse(manifestData.Body, manifestData.Url)
   if err != nil {
      return err
   }

   group, ok := mpd.GetRepresentations()[streamId]
   if !ok {
      return fmt.Errorf("representation group not found %v", streamId)
   }

   var totalBits, totalSeconds float64
   for _, rep := range group {
      if rep.SegmentBase == nil {
         return fmt.Errorf("stream %v is not SegmentBase", rep.Id)
      }

      baseUrl, err := rep.ResolveBaseUrl()
      if err != nil {
         return err
      }
      sidxData, err := fetchData(baseUrl, map[string]string{
         "Range": "bytes=" + rep.SegmentBase.IndexRange,
      }, true)
      if err != nil {
         return err
      }

      boxes, err := sofia.DecodeBoxes(sidxData)
      if err != nil {
         return err
      }
      sidx, ok := sofia.FindSidx(boxes)
      if !ok {
         return errors.New("box 'sidx' not found")
      }

      var repSeconds float64
      for _, ref := range sidx.References {
         if ref.ReferenceType {
            return errors.New("sidx references a child sidx")
         }
         totalBits += float64(ref.ReferencedSize) * 8
         repSeconds += float64(ref.SubsegmentDuration) / float64(sidx.Timescale)
      }
      totalSeconds += repSeconds
   }

   if totalSeconds == 0 {
      return errors.New("zero duration in sidx")
   }
   log.Printf("bitrate: %.0f b/s", totalBits/totalSeconds)
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

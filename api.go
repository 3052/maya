// api.go
package maya

import (
   "41.neocities.org/luna/dash"
   "41.neocities.org/luna/hls"
   "bytes"
   "errors"
   "io"
   "log"
   "net/http"
   "net/url"
   "strings"
   "sync/atomic"
)

// Get performs an HTTP GET request by manually constructing the http.Request
func Get(targetUrl *url.URL, headers map[string]string) (*http.Response, error) {
   reqHeader := make(http.Header)
   for key, value := range headers {
      reqHeader.Set(key, value)
   }
   req := &http.Request{
      Method: http.MethodGet,
      URL:    targetUrl,
      Header: reqHeader,
   }

   log.Println(req.Method, req.URL)
   return http.DefaultClient.Do(req)
}

// Post performs an HTTP POST request by manually constructing the http.Request
func Post(targetUrl *url.URL, headers map[string]string, body []byte) (*http.Response, error) {
   reqHeader := make(http.Header)
   for key, value := range headers {
      reqHeader.Set(key, value)
   }
   req := &http.Request{
      Method: http.MethodPost,
      URL:    targetUrl,
      Header: reqHeader,
   }
   if len(body) >= 1 {
      req.Body = io.NopCloser(bytes.NewReader(body))
   }

   log.Println(req.Method, req.URL)
   return http.DefaultClient.Do(req)
}

func Head(targetUrl *url.URL, headers map[string]string) (*http.Response, error) {
   reqHeader := make(http.Header)
   for key, value := range headers {
      reqHeader.Set(key, value)
   }
   req := &http.Request{
      Method: http.MethodHead,
      URL:    targetUrl,
      Header: reqHeader,
   }

   log.Println(req.Method, req.URL)
   return http.DefaultClient.Do(req)
}

// getBytes performs an HTTP GET request and returns its body.
func getBytes(targetUrl *url.URL, byteRange string) ([]byte, error) {
   req := http.Request{
      URL:    targetUrl,
      Header: make(http.Header),
   }
   if byteRange != "" {
      req.Header.Set("Range", "bytes="+byteRange)
   }

   resp, err := http.DefaultClient.Do(&req)
   if err != nil {
      return nil, err
   }
   defer resp.Body.Close()

   if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
      return nil, errors.New(resp.Status)
   }
   return io.ReadAll(resp.Body)
}

// SetProxy overrides the global http.DefaultTransport with the proxy routing
// logic
func SetProxy(proxiesCsv string) error {
   if proxiesCsv != "" {
      prt := &proxyRoundTripper{}

      for _, proxyStr := range strings.Split(proxiesCsv, ",") {
         parsedUrl, err := url.Parse(proxyStr)
         if err != nil {
            return err // Standard Go short-circuit on error
         }

         transport := &http.Transport{}
         transport.Proxy = http.ProxyURL(parsedUrl)
         prt.transports = append(prt.transports, transport)
      }

      log.Println("overriding http.DefaultTransport with proxies")
      http.DefaultTransport = prt
   }

   return nil
}

func (p *proxyRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
   // Safely increment the index.
   idx := atomic.AddUint32(&p.index, 1)

   // Safely select the transport
   transport := p.transports[int(idx)%len(p.transports)]

   return transport.RoundTrip(req)
}

type proxyRoundTripper struct {
   transports []*http.Transport
   index      uint32
}

type Manifest struct {
   Url  *url.URL
   Body []byte
}

type DrmSystem int

const (
   DrmNone DrmSystem = iota
   DrmPlayReady
   DrmWidevine
)

type Device string

type Options struct {
   Threads int
   Drm     DrmSystem
   Device  Device
   License func([]byte) ([]byte, error)
}

func ListDash(baseUrl *url.URL) (*Manifest, error) {
   resp, err := Get(baseUrl, nil)
   if err != nil {
      return nil, err
   }
   defer resp.Body.Close()
   if resp.StatusCode != http.StatusOK {
      return nil, errors.New(resp.Status)
   }

   body, err := io.ReadAll(resp.Body)
   if err != nil {
      return nil, err
   }

   finalUrl := resp.Request.URL
   manifest, err := dash.Parse(body, finalUrl)
   if err != nil {
      return nil, err
   }

   if err := listStreamsDash(manifest); err != nil {
      return nil, err
   }

   return &Manifest{Url: finalUrl, Body: body}, nil
}

func ListHls(baseUrl *url.URL) (*Manifest, error) {
   resp, err := Get(baseUrl, nil)
   if err != nil {
      return nil, err
   }
   defer resp.Body.Close()
   if resp.StatusCode != http.StatusOK {
      return nil, errors.New(resp.Status)
   }

   body, err := io.ReadAll(resp.Body)
   if err != nil {
      return nil, err
   }

   finalUrl := resp.Request.URL
   playlist, err := hls.DecodeMaster(string(body), finalUrl)
   if err != nil {
      return nil, err
   }
   if err := listStreamsHls(playlist); err != nil {
      return nil, err
   }

   return &Manifest{Url: finalUrl, Body: body}, nil
}

func DownloadDash(streamId string, manifestData *Manifest, optionsData *Options) error {
   if optionsData == nil {
      optionsData = &Options{}
   }

   manifest, err := dash.Parse(manifestData.Body, manifestData.Url)
   if err != nil {
      return err
   }

   kFetcher, err := optionsData.getKeyFetcher()
   if err != nil {
      return err
   }

   return downloadDash(manifest, optionsData.Threads, streamId, kFetcher)
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

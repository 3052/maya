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
   "sync"
   "sync/atomic"
)

var (
   activeProxies  []string
   logProxiesOnce sync.Once
)

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

         activeProxies = append(activeProxies, proxyStr)
      }

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

// doRequest is an internal helper to construct and execute requests with optional logging
func doRequest(method string, targetUrl *url.URL, headers map[string]string, body []byte, logReq bool) (*http.Response, error) {
   logProxiesOnce.Do(func() {
      for _, p := range activeProxies {
         log.Println("proxy:", p)
      }
   })

   reqHeader := make(http.Header)
   for key, value := range headers {
      reqHeader.Set(key, value)
   }
   req := &http.Request{
      Method: method,
      URL:    targetUrl,
      Header: reqHeader,
   }
   if len(body) >= 1 {
      req.Body = io.NopCloser(bytes.NewReader(body))
   }

   if logReq {
      log.Println(req.Method, req.URL)
   }
   return http.DefaultClient.Do(req)
}

// Get performs an HTTP GET request and logs it
func Get(targetUrl *url.URL, headers map[string]string) (*http.Response, error) {
   return doRequest(http.MethodGet, targetUrl, headers, nil, true)
}

// Post performs an HTTP POST request and logs it
func Post(targetUrl *url.URL, headers map[string]string, body []byte) (*http.Response, error) {
   return doRequest(http.MethodPost, targetUrl, headers, body, true)
}

// Head performs an HTTP HEAD request and logs it
func Head(targetUrl *url.URL, headers map[string]string) (*http.Response, error) {
   return doRequest(http.MethodHead, targetUrl, headers, nil, true)
}

// FetchData performs an HTTP GET request, checks the status code, and returns the response body
func FetchData(targetUrl *url.URL, headers map[string]string) ([]byte, error) {
   return fetchData(targetUrl, headers, true)
}

// FetchDataSilent performs an HTTP GET request without logging it
func FetchDataSilent(targetUrl *url.URL, headers map[string]string) ([]byte, error) {
   return fetchData(targetUrl, headers, false)
}

func fetchData(targetUrl *url.URL, headers map[string]string, logReq bool) ([]byte, error) {
   resp, err := doRequest(http.MethodGet, targetUrl, headers, nil, logReq)
   if err != nil {
      return nil, err
   }
   defer resp.Body.Close()

   if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
      return nil, errors.New(resp.Status)
   }
   return io.ReadAll(resp.Body)
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

type Options struct {
   Threads int
   Drm     DrmSystem
   Device  string
   License func([]byte) ([]byte, error)
}

func ListDash(baseUrl *url.URL) (*Manifest, error) {
   body, err := FetchData(baseUrl, nil)
   if err != nil {
      return nil, err
   }

   manifest, err := dash.Parse(body, baseUrl)
   if err != nil {
      return nil, err
   }

   if err := listStreamsDash(manifest); err != nil {
      return nil, err
   }

   return &Manifest{Url: baseUrl, Body: body}, nil
}

func ListHls(baseUrl *url.URL) (*Manifest, error) {
   body, err := FetchData(baseUrl, nil)
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

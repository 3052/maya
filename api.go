// api.go
package maya

import (
   "bytes"
   "errors"
   "io"
   "log"
   "net/http"
   "net/url"
   "strings"
   "sync/atomic"
)

// SetProxy overrides the global http.DefaultTransport with the proxy routing logic.
func SetProxy(proxiesCsv string) error {
   prt := &proxyRoundTripper{}

   if proxiesCsv != "" {
      for _, proxyStr := range strings.Split(proxiesCsv, ",") {
         parsedUrl, err := url.Parse(proxyStr)
         if err != nil {
            return err
         }
         transport := &http.Transport{}
         transport.Proxy = http.ProxyURL(parsedUrl)
         prt.transports = append(prt.transports, transport)
      }

      // Log without the variable
      log.Println("Overriding http.DefaultTransport with proxies")
   } else {
      prt.transports = []*http.Transport{{}}
   }

   http.DefaultTransport = prt
   return nil
}

func (p *proxyRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
   // Safely increment the index across multiple goroutines
   idx := atomic.AddUint32(&p.index, 1) - 1

   // Safely select the transport
   transport := p.transports[int(idx)%len(p.transports)]

   return transport.RoundTrip(req)
}

type proxyRoundTripper struct {
   transports []*http.Transport
   index      uint32
}

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

// typeInfo holds the determined properties of a media stream.
type typeInfo struct {
   Extension string
   IsFmp4    bool
}

type ManifestGetter func() (*url.URL, error)

func ListDash(getter ManifestGetter) (*Dash, error) {
   baseUrl, err := getter()
   if err != nil {
      return nil, err
   }
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
   manifest, err := parseDash(body, finalUrl)
   if err != nil {
      return nil, err
   }

   if err := listStreamsDash(manifest); err != nil {
      return nil, err
   }

   return &Dash{Url: finalUrl, Body: body}, nil
}

func ListHls(getter ManifestGetter) (*Hls, error) {
   baseUrl, err := getter()
   if err != nil {
      return nil, err
   }
   resp, err := Get(baseUrl, nil)
   if err != nil {
      return nil, err
   }
   defer resp.Body.Close()
   if resp.StatusCode != http.StatusOK {
      return nil, errors.New(resp.Status)
   }
   var builder strings.Builder
   _, err = io.Copy(&builder, resp.Body)
   if err != nil {
      return nil, err
   }
   body := builder.String()
   finalUrl := resp.Request.URL
   playlist, err := parseHls(body, finalUrl)
   if err != nil {
      return nil, err
   }
   if err := listStreamsHls(playlist); err != nil {
      return nil, err
   }
   return &Hls{Url: finalUrl, Body: body}, nil
}

type Dash struct {
   Url  *url.URL
   Body []byte
}

func (m *Dash) Download(jobSetup *Job, fetcher LicenseFetcher) error {
   if jobSetup == nil {
      jobSetup = &Job{}
   }

   manifest, err := parseDash(m.Body, m.Url)
   if err != nil {
      return err
   }

   kFetcher, err := jobSetup.getFetcher(fetcher)
   if err != nil {
      return err
   }

   return downloadDash(manifest, jobSetup.Threads, jobSetup.Dash, kFetcher)
}

type Hls struct {
   Url  *url.URL
   Body string
}

func (m *Hls) Download(jobSetup *Job, fetcher LicenseFetcher) error {
   if jobSetup == nil {
      jobSetup = &Job{}
   }

   playlist, err := parseHls(m.Body, m.Url)
   if err != nil {
      return err
   }

   kFetcher, err := jobSetup.getFetcher(fetcher)
   if err != nil {
      return err
   }

   return downloadHls(playlist, jobSetup.Threads, jobSetup.Hls, kFetcher)
}

type LicenseFetcher func([]byte) ([]byte, error)

type Job struct {
   Threads   int
   Widevine  string
   PlayReady string
   Dash      string
   Hls       int
}

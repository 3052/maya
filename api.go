// api.go
package maya

import (
   "bytes"
   "encoding/xml"
   "errors"
   "fmt"
   "io"
   "log"
   "net/http"
   "net/url"
   "os"
   "path"
   "path/filepath"
   "slices"
   "strconv"
   "strings"
)

// Get performs an HTTP GET request by manually constructing the http.Request
// struct
func Get(targetURL *url.URL, headers map[string]string) (*http.Response, error) {
   // Initialize a non-nil http.Header to prevent default-client panics.
   // Ranging over a nil 'headers' map is safe in Go.
   reqHeader := make(http.Header)
   for key, value := range headers {
      reqHeader.Set(key, value)
   }
   req := &http.Request{
      Method: http.MethodGet,
      URL:    targetURL,
      Header: reqHeader,
   }
   return http.DefaultClient.Do(req)
}

// Post performs an HTTP POST request by manually constructing the http.Request
// struct
func Post(targetURL *url.URL, headers map[string]string, body *bytes.Buffer) (*http.Response, error) {
   reqHeader := make(http.Header)
   for key, value := range headers {
      reqHeader.Set(key, value)
   }
   req := &http.Request{
      Method: http.MethodPost,
      URL:    targetURL,
      Header: reqHeader,
   }
   if body != nil {
      req.Body = io.NopCloser(body)
   }
   return http.DefaultClient.Do(req)
}

func (p *proxyRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
   // An empty method implies "GET". Update the request directly.
   if req.Method == "" {
      req.Method = http.MethodGet
   }

   logReq, err := p.shouldLog(req.URL.Path)
   if err != nil {
      return nil, err // Abort the request and return the pattern error
   }

   // Grab the current transport using the stored index
   transport := p.transports[p.index]
   if logReq {
      if transport.Proxy != nil {
         log.Printf("proxy %s %s", req.Method, req.URL)
      } else {
         log.Printf("%s %s", req.Method, req.URL)
      }

   }
   // Advance the index for the next request, wrapping around back to 0 when it reaches the end
   p.index = (p.index + 1) % len(p.transports)
   return transport.RoundTrip(req)
}

// overrides the global http.DefaultTransport with the proxy routing logic.
// proxiesCSV is a comma-separated string. ignoreLog accepts multiple string patterns.
func SetProxy(proxiesCSV string, ignoreLog ...string) error {
   prt := &proxyRoundTripper{
      // Directly assign the variadic slice (it will be nil if no args are
      // passed, which is perfectly safe to iterate over in Go)
      ignoreLog: ignoreLog,
   }

   // Parse the proxies
   if proxiesCSV == "" {
      // Directly assign a slice containing exactly one empty transport (no proxy)
      prt.transports = []*http.Transport{{}}
   } else {
      // Append to the slice dynamically
      for _, proxyStr := range strings.Split(proxiesCSV, ",") {
         parsedURL, err := url.Parse(proxyStr)
         if err != nil {
            return err // Do not ignore URL parsing errors
         }

         transport := &http.Transport{}
         transport.Proxy = http.ProxyURL(parsedURL)
         prt.transports = append(prt.transports, transport)
      }

   }
   // Assign our custom RoundTripper to the global DefaultTransport
   http.DefaultTransport = prt
   return nil
}

// proxyRoundTripper intercepts requests, logs them, and routes them to the correct transport.
type proxyRoundTripper struct {
   transports []*http.Transport
   index      int
   ignoreLog  []string
}

// shouldLog checks if the request path matches any of the ignore patterns.
// If a pattern is malformed, it returns the error to be handled by the caller.
func (p *proxyRoundTripper) shouldLog(reqPath string) (bool, error) {
   base := path.Base(reqPath)

   for _, pattern := range p.ignoreLog {
      matched, err := path.Match(pattern, base)
      if err != nil {
         return false, err
      }

      if matched {
         return false, nil
      }

   }

   return true, nil
}

type Flag struct {
   Name   string
   IsBool bool
   IsSet  bool
   Set    func(string) error
   Usage  string
}

var flags []*Flag

func FuncFlag(name, usage string, fn func(string) error) *Flag {
   option := &Flag{
      Name:  name,
      Set:   fn,
      Usage: fmt.Sprintf(" value\n\t%s", usage),
   }

   flags = append(flags, option)
   return option
}

func StringFlag(pointer *string, name, usage string) *Flag {
   usage = fmt.Sprintf(" string\n\t%s", usage)
   if *pointer != "" {
      usage += fmt.Sprintf("\n\tdefault %s", *pointer)
   }

   option := &Flag{
      Name: name,
      Set: func(val string) error {
         *pointer = val
         return nil
      },
      Usage: usage,
   }

   flags = append(flags, option)
   return option
}

func BoolFlag(name, usage string) *Flag {
   option := &Flag{
      Name:   name,
      IsBool: true,
      Usage:  fmt.Sprintf("\n\t%s", usage),
   }

   flags = append(flags, option)
   return option
}

func IntFlag(pointer *int, name, usage string) *Flag {
   usage = fmt.Sprintf(" int\n\t%s", usage)
   if *pointer != 0 {
      usage += fmt.Sprintf("\n\tdefault %d", *pointer)
   }

   option := &Flag{
      Name: name,
      Set: func(val string) (err error) {
         *pointer, err = strconv.Atoi(val)
         return
      },
      Usage: usage,
   }

   flags = append(flags, option)
   return option
}

func ParseFlags() error {
   for index := 1; index < len(os.Args); index++ {
      arg := os.Args[index]

      if len(arg) < 2 || arg[0] != '-' {
         return fmt.Errorf("unexpected argument: %s", arg)
      }

      name := arg[1:]

      idx := slices.IndexFunc(flags, func(option *Flag) bool {
         return option.Name == name
      })

      if idx == -1 {
         return fmt.Errorf("provided but not defined: -%s", name)
      }
      option := flags[idx]

      if !option.IsBool {
         index++
         if index >= len(os.Args) {
            return fmt.Errorf("flag needs an argument: -%s", name)
         }

         if err := option.Set(os.Args[index]); err != nil {
            return fmt.Errorf("invalid value for flag -%s: %v", name, err)
         }
      }

      option.IsSet = true

   }

   return nil
}

func PrintFlags(groups [][]*Flag) error {
   printed := make(map[*Flag]bool)

   for index, group := range groups {
      if index > 0 {
         fmt.Fprintln(os.Stderr)
      }

      for _, option := range group {
         fmt.Fprintf(os.Stderr, "-%s%s\n", option.Name, option.Usage)
         printed[option] = true
      }

   }

   for _, option := range flags {
      if !printed[option] {
         return fmt.Errorf("flag -%s is missing from PrintFlags groups", option.Name)
      }

   }
   return nil
}

func (c *Cache) Read(value any) func(func() error) error {
   // 1. Attempt the read and unmarshal, capturing any error
   data, err := os.ReadFile(c.File)
   if err == nil {
      err = xml.Unmarshal(data, value)
   }

   // 2. Return the callback wrapper
   return func(action func() error) error {
      if err != nil {
         return err // Blocks the action and returns the read error
      }

      return action()
   }

}

type Cache struct {
   File string
}

func (c *Cache) Setup(file string) error {
   var err error
   c.File, err = os.UserCacheDir()
   if err != nil {
      return err
   }

   c.File = filepath.Join(c.File, file)
   return os.MkdirAll(filepath.Dir(c.File), os.ModePerm)
}

func (c *Cache) Write(value any) error {
   data, err := xml.MarshalIndent(value, "", " ")
   if err != nil {
      return err
   }

   log.Println("Write", c.File)
   return os.WriteFile(c.File, data, os.ModePerm)
}

// ManifestGetter defines a function for retrieving the manifest URL.
type ManifestGetter func() (*url.URL, error)

// ListDash fetches, parses a DASH manifest, and lists the available streams.
func ListDash(getter ManifestGetter) (*Dash, error) {
   baseURL, err := getter()
   if err != nil {
      return nil, err
   }

   request := http.Request{URL: baseURL}
   resp, err := http.DefaultClient.Do(&request)
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

   finalURL := resp.Request.URL
   manifest, err := parseDash(body, finalURL)
   if err != nil {
      return nil, err
   }

   if err := listStreamsDash(manifest); err != nil {
      return nil, err
   }

   return &Dash{Url: finalURL, Body: body}, nil
}

// ListHls fetches, parses an HLS playlist, and lists the available streams.
func ListHls(getter ManifestGetter) (*Hls, error) {
   baseURL, err := getter()
   if err != nil {
      return nil, err
   }

   request := http.Request{URL: baseURL}
   resp, err := http.DefaultClient.Do(&request)
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

   finalURL := resp.Request.URL
   playlist, err := parseHls(body, finalURL)
   if err != nil {
      return nil, err
   }

   if err := listStreamsHls(playlist); err != nil {
      return nil, err
   }

   return &Hls{Url: finalURL, Body: body}, nil
}

// Dash holds the DASH manifest response body and the final resolved URL.
type Dash struct {
   Url  *url.URL
   Body []byte
}

// Download parses and downloads a DASH stream (Clear or Encrypted).
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

// Hls holds the HLS playlist response body and the final resolved URL.
type Hls struct {
   Url  *url.URL
   Body string
}

// Download parses and downloads an HLS stream (Clear or Encrypted).
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

// LicenseFetcher encapsulates the process of fetching a byte payload (like a signed
// license request) from a DRM server and returning the response payload.
type LicenseFetcher func([]byte) ([]byte, error)

// Job holds configuration for downloads.
// Widevine and PlayReady specify folder paths containing their respective keys.
type Job struct {
   Threads   int
   Widevine  string
   PlayReady string
   Dash      string
   Hls       int
}

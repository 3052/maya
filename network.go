package maya

import (
   "errors"
   "io"
   "log"
   "net/http"
   "net/url"
   "strings"
)

// Transport configures the default HTTP transport for logging and proxy support.
func Transport(policy func(*http.Request) string) {
   http.DefaultTransport = &http.Transport{
      Protocols: &http.Protocols{}, // github.com/golang/go/issues/25793
      Proxy: func(req *http.Request) (*url.URL, error) {
         flags := policy(req)
         if strings.ContainsRune(flags, 'L') {
            method := req.Method
            if method == "" {
               method = http.MethodGet
            }
            log.Println(method, req.URL)
         }
         if strings.ContainsRune(flags, 'P') {
            return http.ProxyFromEnvironment(req)
         }
         return nil, nil
      },
   }
}

// getSegment performs an HTTP GET request for a segment and returns its body.
func getSegment(targetUrl *url.URL, header http.Header) ([]byte, error) {
   req := http.Request{URL: targetUrl}
   if header != nil {
      req.Header = header
   } else {
      req.Header = http.Header{}
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

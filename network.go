package maya

import (
   "errors"
   "io"
   "log"
   "net/http"
   "net/url"
   "strings"
)

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

// getContentLength discovers the size of a remote resource in bytes.
func getContentLength(targetUrl *url.URL) (int64, error) {
   // 1. Try HEAD
   resp, err := http.Head(targetUrl.String())
   if err != nil {
      return 0, err
   }
   resp.Body.Close()
   if resp.StatusCode == http.StatusOK && resp.ContentLength > 0 {
      return resp.ContentLength, nil
   }

   // 2. Fallback to GET
   if resp.StatusCode == http.StatusMethodNotAllowed || resp.ContentLength <= 0 {
      resp, err = http.Get(targetUrl.String())
      if err != nil {
         return 0, err
      }
      defer resp.Body.Close()
      if resp.ContentLength > 0 {
         return resp.ContentLength, nil
      }
      // 3. Read body manually if Content-Length header is missing
      return io.Copy(io.Discard, resp.Body)
   }

   return 0, errors.New(resp.Status)
}

// Transport configures the default HTTP transport for logging and proxy support.
func Transport(policy func(*http.Request) string) {
   http.DefaultTransport = &http.Transport{
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

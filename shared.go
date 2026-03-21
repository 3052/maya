// shared.go
package maya

import (
   "errors"
   "io"
   "net/http"
   "net/url"
)

// typeInfo holds the determined properties of a media stream.
type typeInfo struct {
   Extension string
   IsFMP4    bool
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

package net

import (
   "encoding/hex"
   "errors"
   "fmt"
   "log"
   "net/http"
   "net/url"
)

// github.com/golang/go/issues/25793
func Transport(policy func(*http.Request) byte) *http.Transport {
   return &http.Transport{
      Protocols: &http.Protocols{},
      Proxy: func(req *http.Request) (*url.URL, error) {
         switch policy(req) {
         case 'L':
            log.Println(req.Method, req.URL)
            return nil, nil
         case 'P':
            return http.ProxyFromEnvironment(req)
         case 'L' + 'P':
            log.Println(req.Method, req.URL)
            return http.ProxyFromEnvironment(req)
         }
         return nil, nil
      },
   }
}

var (
   ErrMissingTraf = errors.New("missing traf box")
   ErrKeyMismatch = errors.New("key ID mismatch")
)

var widevineID, _ = hex.DecodeString("edef8ba979d64acea3c827dcd51d21ed")

// indexRange handles the byte range parsing for SegmentBase
type indexRange [2]uint64

func (i *indexRange) Set(data string) error {
   _, err := fmt.Sscanf(data, "%v-%v", &i[0], &i[1])
   return err
}

func (i *indexRange) String() string {
   return fmt.Sprintf("%v-%v", i[0], i[1])
}

// Internal types for the worker pool
type mediaRequest struct {
   url    *url.URL
   header http.Header
}

type job struct {
   index   int
   request mediaRequest
}

type result struct {
   index int
   data  []byte
   err   error
}

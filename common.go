package net

import (
   "41.neocities.org/dash"
   "41.neocities.org/sofia"
   "encoding/hex"
   "errors"
   "fmt"
   "log"
   "net/http"
   "net/url"
   "strings"
)

// getMediaRequests now only returns the requests and an error.
// It uses sidx internally for SegmentBase calculation but does not return the raw bytes.
func getMediaRequests(group []*dash.Representation) ([]mediaRequest, error) {
   var requests []mediaRequest
   // Local flag/cache to ensure we only process the sidx once per group if needed
   var sidxProcessed bool
   for _, rep := range group {
      baseURL, err := rep.ResolveBaseURL()
      if err != nil {
         return nil, err
      }
      if template := rep.GetSegmentTemplate(); template != nil {
         addrs, err := template.GetSegmentURLs(rep)
         if err != nil {
            return nil, err
         }
         for _, addr := range addrs {
            requests = append(requests, mediaRequest{url: addr})
         }
      } else if rep.SegmentList != nil {
         for _, seg := range rep.SegmentList.SegmentURLs {
            addr, err := seg.ResolveMedia()
            if err != nil {
               return nil, err
            }
            requests = append(requests, mediaRequest{url: addr})
         }
      } else if rep.SegmentBase != nil {
         if sidxProcessed {
            continue
         }
         head := http.Header{}
         // sidx
         head.Set("Range", "bytes="+rep.SegmentBase.IndexRange)
         sidxData, err := getSegment(baseURL, head)
         if err != nil {
            return nil, err
         }
         parsed, err := sofia.Parse(sidxData)
         if err != nil {
            return nil, err
         }
         sidx, ok := sofia.FindSidx(parsed)
         if !ok {
            return nil, sofia.Missing("sidx")
         }
         sidxProcessed = true
         // segments
         var idx indexRange
         err = idx.Set(rep.SegmentBase.IndexRange)
         if err != nil {
            return nil, err
         }
         // Anchor point is the byte immediately following the sidx box.
         // idx[1] is the end byte of the sidx box.
         currentOffset := idx[1] + 1
         for _, ref := range sidx.References {
            idx[0] = currentOffset
            idx[1] = currentOffset + uint64(ref.ReferencedSize) - 1
            h := make(http.Header)
            h.Set("Range", "bytes="+idx.String())
            requests = append(requests,
               mediaRequest{url: baseURL, header: h},
            )
            currentOffset += uint64(ref.ReferencedSize)
         }
      } else {
         requests = append(requests, mediaRequest{url: baseURL})
      }
   }
   return requests, nil
}

// github.com/golang/go/issues/25793
func Transport(policy func(*http.Request) string) {
   http.DefaultTransport = &http.Transport{
      Protocols: &http.Protocols{},
      Proxy: func(req *http.Request) (*url.URL, error) {
         flags := policy(req)
         if strings.ContainsRune(flags, 'L') {
            log.Println(req.Method, req.URL)
         }
         if strings.ContainsRune(flags, 'P') {
            return http.ProxyFromEnvironment(req)
         }
         return nil, nil
      },
   }
}

var (
   errKeyMismatch = errors.New("key ID mismatch")
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

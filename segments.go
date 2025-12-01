package net

import (
   "41.neocities.org/dash"
   "41.neocities.org/sofia"
   "errors"
   "fmt"
   "io"
   "log"
   "net/http"
   "net/url"
   "strings"
)

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

// indexRange handles the byte range parsing for SegmentBase
type indexRange [2]uint64

func (i *indexRange) Set(data string) error {
   _, err := fmt.Sscanf(data, "%v-%v", &i[0], &i[1])
   return err
}

func (i *indexRange) String() string {
   return fmt.Sprintf("%v-%v", i[0], i[1])
}

// downloadInitialization downloads the initialization segment bytes.
func (c *Config) downloadInitialization(media *mediaFile, rep *dash.Representation) ([]byte, error) {
   var targetURL *url.URL
   var head http.Header
   var err error
   // 1. Resolve the Initialization URL and Headers based on the manifest type
   if rep.SegmentBase != nil {
      head = make(http.Header)
      head.Set("Range", "bytes="+rep.SegmentBase.Initialization.Range)
      targetURL, err = rep.ResolveBaseURL()
   } else if tmpl := rep.GetSegmentTemplate(); tmpl != nil && tmpl.Initialization != "" {
      targetURL, err = tmpl.ResolveInitialization(rep)
   } else if rep.SegmentList != nil {
      targetURL, err = rep.SegmentList.Initialization.ResolveSourceURL()
   }
   // 2. Handle errors or early exit if no init segment exists
   if err != nil {
      return nil, err
   }
   if targetURL == nil {
      return nil, nil
   }
   // 3. Download
   return getSegment(targetURL, head)
}

func getSegment(targetURL *url.URL, head http.Header) ([]byte, error) {
   req, err := http.NewRequest("GET", targetURL.String(), nil)
   if err != nil {
      return nil, err
   }
   if head != nil {
      req.Header = head
   }
   resp, err := http.DefaultClient.Do(req)
   if err != nil {
      return nil, err
   }
   defer resp.Body.Close()
   if resp.StatusCode != http.StatusOK {
      if resp.StatusCode != http.StatusPartialContent {
         var msg strings.Builder
         io.Copy(&msg, resp.Body)
         return nil, fmt.Errorf("status %s: %s", resp.Status, msg.String())
      }
   }
   return io.ReadAll(resp.Body)
}

// getMiddleBitrate calculates the bitrate of the middle segment and updates the Representation.
func getMiddleBitrate(rep *dash.Representation) error {
   baseURL, err := rep.ResolveBaseURL()
   if err != nil {
      return err
   }
   log.Println("update", rep.ID)

   // Strategy 1: SegmentBase (Single file with sidx)
   if rep.SegmentBase != nil {
      head := http.Header{}
      head.Set("Range", "bytes="+rep.SegmentBase.IndexRange)
      // reuse getSegment from config.go (same package)
      sidxData, err := getSegment(baseURL, head)
      if err != nil {
         return err
      }
      parsed, err := sofia.Parse(sidxData)
      if err != nil {
         return err
      }
      sidx, ok := sofia.FindSidx(parsed)
      if !ok {
         return sofia.Missing("sidx")
      }
      if len(sidx.References) == 0 {
         return errors.New("no references in sidx")
      }
      // Find Middle Segment
      midIdx := len(sidx.References) / 2
      ref := sidx.References[midIdx]
      sizeBits := uint64(ref.ReferencedSize) * 8
      // duration is in timescale units
      durationSec := float64(ref.SubsegmentDuration) / float64(sidx.Timescale)
      if durationSec <= 0 {
         return errors.New("invalid duration")
      }
      // Update Representation
      rep.Bandwidth = int(float64(sizeBits) / durationSec)
      return nil
   }
   // Strategy 2: SegmentTemplate or SegmentList (Multiple files)
   var urls []*url.URL
   var durationSec float64
   if tmpl := rep.GetSegmentTemplate(); tmpl != nil {
      u, err := tmpl.GetSegmentURLs(rep)
      if err != nil {
         return err
      }
      urls = u
      // Calculate Duration for the middle segment
      midIdx := len(urls) / 2
      timescale := float64(tmpl.GetTimescale())
      if tmpl.SegmentTimeline != nil {
         currentIndex := 0
         found := false
         for _, s := range tmpl.SegmentTimeline.S {
            count := 1
            if s.R > 0 {
               count += s.R
            }
            if midIdx < currentIndex+count {
               durationSec = float64(s.D) / timescale
               found = true
               break
            }
            currentIndex += count
         }
         if !found {
            return errors.New("could not find duration in timeline")
         }
      } else if tmpl.Duration > 0 {
         durationSec = float64(tmpl.Duration) / timescale
      } else {
         return errors.New("unknown segment duration")
      }
   } else if list := rep.SegmentList; list != nil {
      for _, seg := range list.SegmentURLs {
         u, err := seg.ResolveMedia()
         if err != nil {
            return err
         }
         urls = append(urls, u)
      }
      if list.Duration == 0 {
         return errors.New("unknown segment duration")
      }
      durationSec = float64(list.Duration) / float64(list.GetTimescale())
   }
   if len(urls) == 0 {
      rep.Bandwidth = 0
      return nil
   }
   // Fetch Size of Middle Segment
   midIdx := len(urls) / 2
   targetURL := urls[midIdx]
   resp, err := http.DefaultClient.Head(targetURL.String())
   if err != nil {
      return err
   }
   defer resp.Body.Close()
   if resp.ContentLength <= 0 {
      return errors.New("content length missing")
   }
   sizeBits := uint64(resp.ContentLength) * 8
   if durationSec <= 0 {
      return errors.New("invalid duration")
   }
   // Update Representation
   rep.Bandwidth = int(float64(sizeBits) / durationSec)
   return nil
}

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

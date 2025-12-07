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

// getMiddleBitrate calculates the bitrate of the middle segment and updates
// the Representation
func getMiddleBitrate(rep *dash.Representation) error {
   log.Println("update", rep.ID)
   segs, err := generateSegments(rep)
   if err != nil {
      return err
   }
   if len(segs) == 0 {
      rep.Bandwidth = 0
      return nil
   }

   // Select Middle Segment
   mid := segs[len(segs)/2]
   sizeBits := mid.sizeBits

   // If size is unknown (Template/List), fetch it
   if sizeBits == 0 {
      sizeBytes, err := getContentLength(mid.url)
      if err != nil {
         return err
      }
      if sizeBytes <= 0 {
         return errors.New("content length missing")
      }
      sizeBits = uint64(sizeBytes) * 8
   }

   if mid.duration <= 0 {
      return errors.New("invalid duration")
   }

   // Update Representation
   rep.Bandwidth = int(float64(sizeBits) / mid.duration)

   return nil
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
   index    int
   workerID int
   data     []byte
   err      error
}

// Internal segment representation
type segment struct {
   url      *url.URL
   header   http.Header
   duration float64
   sizeBits uint64
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

// generateSegments centralizes the logic to produce a list of segments for a
// Representation. It handles SegmentBase (sidx), SegmentTemplate
// (timeline/duration), and SegmentList
func generateSegments(rep *dash.Representation) ([]segment, error) {
   baseURL, err := rep.ResolveBaseURL()
   if err != nil {
      return nil, err
   }

   // Strategy 1: SegmentTemplate
   if tmpl := rep.GetSegmentTemplate(); tmpl != nil {
      urls, err := tmpl.GetSegmentURLs(rep)
      if err != nil {
         return nil, err
      }

      segments := make([]segment, len(urls))
      timescale := float64(tmpl.GetTimescale())

      if tmpl.SegmentTimeline != nil {
         // Map timeline entries to generated URLs
         currentIdx := 0
         for _, s := range tmpl.SegmentTimeline.S {
            count := 1
            if s.R > 0 {
               count += s.R
            }
            dur := float64(s.D) / timescale
            for i := 0; i < count; i++ {
               if currentIdx < len(segments) {
                  segments[currentIdx].url = urls[currentIdx]
                  segments[currentIdx].duration = dur
               }
               currentIdx++
            }
         }
         // Note: If timeline implies fewer segments than URLs, tail segments get 0 duration.
      } else {
         // Constant duration
         dur := float64(tmpl.Duration) / timescale
         for i := range segments {
            segments[i].url = urls[i]
            segments[i].duration = dur
         }
      }
      return segments, nil
   }

   // Strategy 2: SegmentList
   if list := rep.SegmentList; list != nil {
      segments := make([]segment, 0, len(list.SegmentURLs))
      dur := float64(list.Duration) / float64(list.GetTimescale())
      for _, seg := range list.SegmentURLs {
         u, err := seg.ResolveMedia()
         if err != nil {
            return nil, err
         }
         segments = append(segments, segment{
            url:      u,
            duration: dur,
         })
      }
      return segments, nil
   }

   // Strategy 3: SegmentBase (sidx)
   if rep.SegmentBase != nil {
      head := http.Header{}
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

      var idx indexRange
      if err := idx.Set(rep.SegmentBase.IndexRange); err != nil {
         return nil, err
      }

      // Anchor point is the byte immediately following the sidx box.
      currentOffset := idx[1] + 1
      segments := make([]segment, len(sidx.References))

      for i, ref := range sidx.References {
         endOffset := currentOffset + uint64(ref.ReferencedSize) - 1
         h := make(http.Header)
         h.Set("Range", fmt.Sprintf("bytes=%d-%d", currentOffset, endOffset))

         segments[i] = segment{
            url:      baseURL,
            header:   h,
            duration: float64(ref.SubsegmentDuration) / float64(sidx.Timescale),
            sizeBits: uint64(ref.ReferencedSize) * 8,
         }
         currentOffset += uint64(ref.ReferencedSize)
      }
      return segments, nil
   }

   // Fallback: Single file (BaseURL) without segmentation
   var duration float64
   // Attempt to retrieve duration from parent Period if available
   if rep.Parent != nil && rep.Parent.Parent != nil {
      if d, err := rep.Parent.Parent.GetDuration(); err == nil {
         duration = d.Seconds()
      }
   }

   return []segment{{url: baseURL, duration: duration}}, nil
}

func getContentLength(targetURL *url.URL) (int64, error) {
   // 1. Try HEAD
   resp, err := http.Head(targetURL.String())
   if err != nil {
      return 0, err
   }
   length := resp.ContentLength
   if err := resp.Body.Close(); err != nil {
      return 0, err
   }
   if length > 0 {
      return length, nil
   }

   // 2. Fallback to GET
   resp, err = http.Get(targetURL.String())
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

// getMediaRequests returns the requests using the unified segment generation logic.
func getMediaRequests(group []*dash.Representation) ([]mediaRequest, error) {
   var requests []mediaRequest
   var sidxProcessed bool

   for _, rep := range group {
      // Optimization: For SegmentBase, the sidx is usually shared across the group.
      // If we processed it once, skip to avoid duplicate downloads.
      if rep.SegmentBase != nil {
         if sidxProcessed {
            continue
         }
      }

      segs, err := generateSegments(rep)
      if err != nil {
         return nil, err
      }

      // Mark sidx as processed if we just handled a SegmentBase rep
      if rep.SegmentBase != nil {
         sidxProcessed = true
      }

      for _, s := range segs {
         requests = append(requests, mediaRequest{
            url:    s.url,
            header: s.header,
         })
      }
   }
   return requests, nil
}

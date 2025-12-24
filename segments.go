package maya

import (
   "41.neocities.org/dash"
   "41.neocities.org/sofia"
   "errors"
   "io"
   "log"
   "net/http"
   "net/url"
)

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
   if resp.StatusCode != http.StatusOK {
      if resp.StatusCode != http.StatusPartialContent {
         return nil, errors.New(resp.Status)
      }
   }
   return io.ReadAll(resp.Body)
}

func getContentLength(targetUrl *url.URL) (int64, error) {
   // 1. Try HEAD
   resp, err := http.Head(targetUrl.String())
   if err != nil {
      return 0, err
   }
   if err := resp.Body.Close(); err != nil {
      return 0, err
   }
   switch resp.StatusCode {
   case http.StatusOK:
      // If 200 OK, check if we got the length right away
      if resp.ContentLength > 0 {
         return resp.ContentLength, nil
      }
   case http.StatusMethodNotAllowed:
      // If 405, we explicitly allow falling through to the GET request below
   default:
      return 0, errors.New(resp.Status)
   }
   // 2. Fallback to GET
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

// getMiddleBitrate calculates the bitrate of the middle segment and updates
// the Representation
func getMiddleBitrate(rep *dash.Representation) error {
   log.Println("update", rep.Id)
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
   workerId int
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

// downloadInitialization downloads the initialization segment bytes.
func (c *Config) downloadInitialization(media *mediaFile, rep *dash.Representation) ([]byte, error) {
   var targetUrl *url.URL
   var header http.Header
   var err error
   // 1. Resolve the Initialization URL and Headers based on the manifest type
   if rep.SegmentBase != nil {
      header = make(http.Header)
      header.Set("Range", "bytes="+rep.SegmentBase.Initialization.Range)
      targetUrl, err = rep.ResolveBaseUrl()
   } else if tmpl := rep.GetSegmentTemplate(); tmpl != nil && tmpl.Initialization != "" {
      targetUrl, err = tmpl.ResolveInitialization(rep)
   } else if rep.SegmentList != nil {
      targetUrl, err = rep.SegmentList.Initialization.ResolveSourceUrl()
   }
   // 2. Handle errors or early exit if no init segment exists
   if err != nil {
      return nil, err
   }
   if targetUrl == nil {
      return nil, nil
   }
   // 3. Download
   return getSegment(targetUrl, header)
}

// generateSegments centralizes the logic to produce a list of segments for a
// Representation. It handles SegmentBase (sidx), SegmentTemplate
// (timeline/duration), and SegmentList
func generateSegments(rep *dash.Representation) ([]segment, error) {
   baseUrl, err := rep.ResolveBaseUrl()
   if err != nil {
      return nil, err
   }
   // Strategy 1: SegmentTemplate
   if tmpl := rep.GetSegmentTemplate(); tmpl != nil {
      urls, err := tmpl.GetSegmentUrls(rep)
      if err != nil {
         return nil, err
      }
      segments := make([]segment, len(urls))
      timescale := float64(tmpl.GetTimescale())
      if tmpl.SegmentTimeline != nil {
         // Map timeline entries to generated URLs
         currentIdx := 0
         for _, entry := range tmpl.SegmentTimeline.S {
            count := 1
            if entry.R > 0 {
               count += entry.R
            }
            dur := float64(entry.D) / timescale
            for repeatIdx := 0; repeatIdx < count; repeatIdx++ {
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
         for segIdx := range segments {
            segments[segIdx].url = urls[segIdx]
            segments[segIdx].duration = dur
         }
      }
      return segments, nil
   }
   // Strategy 2: SegmentList
   if list := rep.SegmentList; list != nil {
      segments := make([]segment, 0, len(list.SegmentUrls))
      dur := float64(list.Duration) / float64(list.GetTimescale())
      for _, seg := range list.SegmentUrls {
         mediaURL, err := seg.ResolveMedia()
         if err != nil {
            return nil, err
         }
         segments = append(segments, segment{
            url:      mediaURL,
            duration: dur,
         })
      }
      return segments, nil
   }
   // Strategy 3: SegmentBase (sidx)
   if rep.SegmentBase != nil {
      header := http.Header{}
      header.Set("Range", "bytes="+rep.SegmentBase.IndexRange)
      sidxData, err := getSegment(baseUrl, header)
      if err != nil {
         return nil, err
      }
      parsed, err := sofia.Parse(sidxData)
      if err != nil {
         return nil, err
      }
      sidx, ok := sofia.FindSidx(parsed)
      if !ok {
         return nil, errors.New("box 'sidx' not found")
      }
      _, end, err := dash.ParseRange(rep.SegmentBase.IndexRange)
      if err != nil {
         return nil, err
      }
      // Anchor point is the byte immediately following the sidx box.
      currentOffset := end + 1
      segments := make([]segment, len(sidx.References))
      for refIdx, ref := range sidx.References {
         endOffset := currentOffset + uint64(ref.ReferencedSize) - 1
         header := make(http.Header)
         header.Set("range", "bytes="+dash.FormatRange(currentOffset, endOffset))
         segments[refIdx] = segment{
            url:      baseUrl,
            header:   header,
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
      if periodDuration, err := rep.Parent.Parent.GetDuration(); err == nil {
         duration = periodDuration.Seconds()
      }
   }
   return []segment{{url: baseUrl, duration: duration}}, nil
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
      for _, seg := range segs {
         requests = append(requests, mediaRequest{
            url:    seg.url,
            header: seg.header,
         })
      }
   }
   return requests, nil
}

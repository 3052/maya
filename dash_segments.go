package maya

import (
   "41.neocities.org/luna/dash"
   "41.neocities.org/sofia"
   "errors"
   "log"
   "net/http"
   "net/url"
   "strings"
)

// Internal segment representation, primarily for DASH's detailed view.
type segment struct {
   url      *url.URL
   header   http.Header
   duration float64
   sizeBits uint64
}

// getDashProtection finds the protection data that matches the requested scheme.
func getDashProtection(rep *dash.Representation, scheme string) (*protectionInfo, error) {
   const widevineURN = "urn:uuid:edef8ba9-79d6-4ace-a3c8-27dcd51d21ed"
   const playreadyURN = "urn:uuid:9a04f079-9840-4286-ab92-e65be0885f95"

   for _, cp := range rep.GetContentProtection() {
      schemeUri := strings.ToLower(cp.SchemeIdUri)
      matches := (scheme == "widevine" && schemeUri == widevineURN) ||
         (scheme == "playready" && schemeUri == playreadyURN)

      if matches {
         pssh, _ := cp.GetPssh()
         kid, _ := cp.GetDefaultKid()
         return &protectionInfo{Pssh: pssh, KeyID: kid}, nil
      }
   }
   return nil, nil // No matching protection data found.
}

// getDashMediaRequests generates the full list of media segments for a DASH group.
func getDashMediaRequests(group []*dash.Representation, sidxData []byte) ([]mediaRequest, error) {
   var requests []mediaRequest
   for _, rep := range group {
      var segs []segment
      var err error
      if rep.SegmentBase != nil {
         segs, err = generateSegmentsFromSidx(rep, sidxData)
      } else {
         segs, err = generateSegments(rep)
      }
      if err != nil {
         return nil, err
      }
      for _, seg := range segs {
         requests = append(requests, mediaRequest{url: seg.url, header: seg.header})
      }
   }
   return requests, nil
}

// getMiddleBitrate calculates the bitrate of the middle segment and updates the Representation.
func getMiddleBitrate(rep *dash.Representation, sidxCache map[string][]byte) error {
   log.Println("update", rep.Id)
   var segs []segment
   var err error

   if rep.SegmentBase != nil {
      baseUrl, err_base := rep.ResolveBaseUrl()
      if err_base != nil {
         return err_base
      }
      cacheKey := baseUrl.String() + rep.SegmentBase.IndexRange
      sidxData, exists := sidxCache[cacheKey]
      if !exists {
         header := http.Header{}
         header.Set("Range", "bytes="+rep.SegmentBase.IndexRange)
         sidxData, err = getSegment(baseUrl, header)
         if err != nil {
            return err
         }
         sidxCache[cacheKey] = sidxData
      }
      segs, err = generateSegmentsFromSidx(rep, sidxData)
   } else {
      segs, err = generateSegments(rep)
   }

   if err != nil {
      return err
   }
   if len(segs) == 0 {
      rep.Bandwidth = 0
      return nil
   }
   mid := segs[len(segs)/2]

   sizeBits := mid.sizeBits
   if sizeBits == 0 {
      data, err_get := getSegment(mid.url, mid.header)
      if err_get != nil {
         return err_get
      }
      sizeBits = uint64(len(data)) * 8
   }

   if mid.duration <= 0 {
      return errors.New("invalid duration for bitrate calculation")
   }

   rep.Bandwidth = int(float64(sizeBits) / mid.duration)
   return nil
}

// generateSegmentsFromSidx parses a pre-fetched sidx box to generate segments.
func generateSegmentsFromSidx(rep *dash.Representation, sidxData []byte) ([]segment, error) {
   baseUrl, err := rep.ResolveBaseUrl()
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

// generateSegments centralizes the logic to produce a list of segments for a
// DASH Representation. It handles SegmentTemplate and SegmentList.
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
      } else {
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
   // Fallback: Single file (BaseURL) without segmentation
   var duration float64
   if rep.Parent != nil && rep.Parent.Parent != nil {
      if periodDuration, err := rep.Parent.Parent.GetDuration(); err == nil {
         duration = periodDuration.Seconds()
      }
   }
   return []segment{{url: baseUrl, duration: duration}}, nil
}

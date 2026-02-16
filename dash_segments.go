package maya

import (
   "41.neocities.org/luna/dash"
   "41.neocities.org/sofia"
   "errors"
   "net/http"
   "net/url"
)

// Internal segment representation, primarily for DASH's detailed view.
type segment struct {
   url      *url.URL
   header   http.Header
   duration float64
   sizeBits uint64
}

// getDashMediaRequests generates the full list of media segments for a DASH representation group.
func getDashMediaRequests(group []*dash.Representation, sidxData []byte) ([]mediaRequest, error) {
   if len(group) == 0 {
      return nil, nil
   }
   // THE FIX: If using SegmentBase, the sidx contains all segments. Process it ONCE.
   if group[0].SegmentBase != nil {
      segs, err := generateSegmentsFromSidx(group[0], sidxData)
      if err != nil {
         return nil, err
      }
      requests := make([]mediaRequest, len(segs))
      for i, seg := range segs {
         requests[i] = mediaRequest{url: seg.url, header: seg.header}
      }
      return requests, nil
   }
   // For other types (SegmentTemplate, SegmentList), iterate through each Period's
   // Representation to build the full list. This logic was correct.
   var requests []mediaRequest
   for _, rep := range group {
      segs, err := generateSegments(rep)
      if err != nil {
         return nil, err
      }
      for _, seg := range segs {
         requests = append(requests, mediaRequest{url: seg.url, header: seg.header})
      }
   }
   return requests, nil
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

// generateSegments centralizes the logic to produce a list of segments.
func generateSegments(rep *dash.Representation) ([]segment, error) {
   baseUrl, err := rep.ResolveBaseUrl()
   if err != nil {
      return nil, err
   }
   if template := rep.GetSegmentTemplate(); template != nil {
      urls, err := template.GetSegmentUrls(rep)
      if err != nil {
         return nil, err
      }
      segments := make([]segment, len(urls))
      timescale := float64(template.GetTimescale())
      if template.SegmentTimeline != nil {
         currentIdx := 0
         for _, entry := range template.SegmentTimeline.S {
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
         dur := float64(template.Duration) / timescale
         for segIdx := range segments {
            segments[segIdx].url = urls[segIdx]
            segments[segIdx].duration = dur
         }
      }
      return segments, nil
   }
   if segmentList := rep.SegmentList; segmentList != nil {
      segments := make([]segment, 0, len(segmentList.SegmentUrls))
      dur := float64(segmentList.Duration) / float64(segmentList.GetTimescale())
      for _, seg := range segmentList.SegmentUrls {
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
   var duration float64
   if rep.Parent != nil && rep.Parent.Parent != nil {
      if periodDuration, err := rep.Parent.Parent.GetDuration(); err == nil {
         duration = periodDuration.Seconds()
      }
   }
   return []segment{{url: baseUrl, duration: duration}}, nil
}

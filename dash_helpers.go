package maya

import (
   "41.neocities.org/luna/dash"
   "41.neocities.org/sofia"
   "errors"
   "fmt"
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

// getDashProtection extracts Widevine PSSH and the default Key ID from a representation.
func getDashProtection(rep *dash.Representation) (*protectionInfo, error) {
   const widevineURN = "urn:uuid:edef8ba9-79d6-4ace-a3c8-27dcd51d21ed"
   var psshData []byte
   var keyID []byte
   for _, contentProtection := range rep.GetContentProtection() {
      if keyID == nil {
         kid, err := contentProtection.GetDefaultKid()
         if err != nil {
            return nil, fmt.Errorf("could not parse default_KID: %w", err)
         }
         if kid != nil {
            keyID = kid
         }
      }
      if strings.ToLower(contentProtection.SchemeIdUri) == widevineURN {
         pssh, err := contentProtection.GetPssh()
         if err != nil {
            return nil, fmt.Errorf("could not parse widevine pssh: %w", err)
         }
         if pssh != nil {
            psshData = pssh
         }
      }
   }
   if keyID == nil {
      return nil, nil
   }
   log.Printf("key ID from manifest: %x", keyID)
   return &protectionInfo{Pssh: psshData, KeyID: keyID}, nil
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

// getMiddleBitrate calculates an accurate bitrate for a representation.
func getMiddleBitrate(rep *dash.Representation, sidxCache map[string][]byte) error {
   log.Println("update", rep.Id)
   if rep.SegmentBase != nil {
      baseUrl, err := rep.ResolveBaseUrl()
      if err != nil {
         return err
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
      segs, err := generateSegmentsFromSidx(rep, sidxData)
      if err != nil {
         return err
      }
      if len(segs) == 0 {
         rep.Bandwidth = 0
         return nil
      }
      var totalSizeBits uint64 = 0
      var totalDuration float64 = 0
      for _, seg := range segs {
         totalSizeBits += seg.sizeBits
         totalDuration += seg.duration
      }
      if totalDuration <= 0 {
         return errors.New("invalid total duration from sidx for bitrate calculation")
      }
      rep.Bandwidth = int(float64(totalSizeBits) / totalDuration)
      return nil
   }
   segs, err := generateSegments(rep)
   if err != nil {
      return err
   }
   if len(segs) == 0 {
      rep.Bandwidth = 0
      return nil
   }
   mid := segs[len(segs)/2]
   data, err := getSegment(mid.url, mid.header)
   if err != nil {
      return err
   }
   sizeBits := uint64(len(data)) * 8
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

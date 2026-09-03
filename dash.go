package maya

import (
   "41.neocities.org/luna/dash"
   "41.neocities.org/sofia"
   "fmt"
   "slices"
)

// downloadDash parses a DASH manifest, extracts all necessary data, and passes it to the central orchestrator.
func downloadDash(mpd *dash.Mpd, threads int, streamId string, fetchKey keyFetcher) error {
   dashGroup, ok := mpd.GetRepresentations()[streamId]
   if !ok {
      return fmt.Errorf("representation group not found %v", streamId)
   }
   if len(dashGroup) == 0 {
      return fmt.Errorf("representation group is empty")
   }
   rep := dashGroup[0]
   info, err := detectDashType(rep)
   if err != nil {
      return err
   }
   var sidxData []byte
   if rep.SegmentBase != nil {
      baseUrl, err := rep.ResolveBaseUrl()
      if err != nil {
         return err
      }
      sidxData, err = fetchData(baseUrl, map[string]string{"Range": "bytes=" + rep.SegmentBase.IndexRange}, true)
      if err != nil {
         return fmt.Errorf("failed to pre-fetch sidx data: %w", err)
      }
   }
   allRequests, err := getDashMediaRequests(dashGroup, sidxData)
   if err != nil {
      return err
   }
   initData, err := getDashInitSegment(rep, info)
   if err != nil {
      return err
   }
   protection, err := getDashProtection(rep)
   if err != nil {
      return err
   }
   job := &downloadJob{
      outputFileNameBase: rep.Id,
      info:               info,
      allRequests:        allRequests,
      initSegmentData:    initData,
      manifestProtection: protection,
      threads:            threads,
      fetchKey:           fetchKey,
   }
   return orchestrateDownload(job)
}

// getDashInitSegment locates and fetches the initialization segment for a DASH representation.
func getDashInitSegment(rep *dash.Representation, info *typeInfo) ([]byte, error) {
   if !info.IsFmp4 {
      return nil, nil
   }
   // Case 1: Initialization defined in SegmentBase
   if rep.SegmentBase != nil && rep.SegmentBase.Initialization != nil {
      baseUrl, err := rep.ResolveBaseUrl()
      if err != nil {
         return nil, err
      }
      return fetchData(baseUrl, map[string]string{"Range": "bytes=" + rep.SegmentBase.Initialization.Range}, true)
   }
   // Case 2: Initialization defined in SegmentTemplate
   if template := rep.GetSegmentTemplate(); template != nil && template.Initialization != "" {
      initUrl, err := template.ResolveInitialization(rep)
      if err != nil {
         return nil, fmt.Errorf("failed to resolve DASH SegmentTemplate initialization URL: %w", err)
      }
      return fetchData(initUrl, nil, true)
   }
   // Case 3: Initialization defined in SegmentList
   if sl := rep.SegmentList; sl != nil && sl.Initialization != nil {
      initUrl, err := sl.Initialization.ResolveSourceUrl()
      if err != nil {
         return nil, fmt.Errorf("failed to resolve DASH SegmentList initialization URL: %w", err)
      }

      var headers map[string]string
      if sl.Initialization.Range != "" {
         headers = map[string]string{"Range": "bytes=" + sl.Initialization.Range}
      }

      return fetchData(initUrl, headers, true)
   }
   return nil, nil
}

// listStreamsDash is an internal helper to print streams from a parsed manifest
func listStreamsDash(mpd *dash.Mpd) error {
   groups := mpd.GetRepresentations()
   repsForSorting := make([]*dash.Representation, 0, len(groups))
   for _, group := range groups {
      representation := group[len(group)/2]
      repsForSorting = append(repsForSorting, representation)
   }
   slices.SortFunc(repsForSorting, func(a, b *dash.Representation) int {
      return a.Bandwidth - b.Bandwidth
   })
   for index, representation := range repsForSorting {
      if index > 0 {
         fmt.Println()
      }
      fmt.Println(representation)
   }
   return nil
}

// detectDashType determines the file extension and container type from a DASH Representation's metadata.
func detectDashType(rep *dash.Representation) (*typeInfo, error) {
   switch rep.GetMimeType() {
   case "video/mp4":
      return &typeInfo{Extension: ".mp4", IsFmp4: true}, nil
   case "audio/mp4":
      return &typeInfo{Extension: ".m4a", IsFmp4: true}, nil
   case "text/vtt":
      return &typeInfo{Extension: ".vtt", IsFmp4: false}, nil
   default:
      return nil, fmt.Errorf("unsupported mime type for stream %s: %s", rep.Id, rep.GetMimeType())
   }
}

// generateSegmentsFromSidx parses a pre-fetched sidx box to generate segments.
func generateSegmentsFromSidx(rep *dash.Representation, sidxData []byte, groupSegments bool) ([]segment, error) {
   baseUrl, err := rep.ResolveBaseUrl()
   if err != nil {
      return nil, err
   }
   sidx, err := sofia.DecodeSidxBox(sidxData)
   if err != nil {
      return nil, err
   }
   _, end, err := dash.ParseRange(rep.SegmentBase.IndexRange)
   if err != nil {
      return nil, err
   }

   var segments []segment
   const targetChunkSize = 2 * 1024 * 1024

   currentOffset := end + 1
   chunkStart := currentOffset
   var chunkDuration float64

   for index, ref := range sidx.References {
      refSize := uint64(ref.ReferencedSize)
      chunkDuration += float64(ref.SubsegmentDuration) / float64(sidx.Timescale)
      currentOffset += refSize

      if !groupSegments || (currentOffset-chunkStart) >= targetChunkSize || index == len(sidx.References)-1 {
         endOffset := currentOffset - 1

         segments = append(segments, segment{
            url:      baseUrl,
            headers:  map[string]string{"Range": "bytes=" + dash.FormatRange(chunkStart, endOffset)},
            duration: chunkDuration,
            sizeBits: (currentOffset - chunkStart) * 8,
         })

         chunkStart = currentOffset
         chunkDuration = 0
      }
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
   if sl := rep.SegmentList; sl != nil {
      segments := make([]segment, 0, len(sl.SegmentUrls))
      dur := float64(sl.Duration) / float64(sl.GetTimescale())
      for _, seg := range sl.SegmentUrls {
         mediaURL, err := seg.ResolveMedia()
         if err != nil {
            return nil, err
         }

         // Check if a byte range is specified for the segment
         var headers map[string]string
         if seg.MediaRange != "" {
            headers = map[string]string{"Range": "bytes=" + seg.MediaRange}
         }

         segments = append(segments, segment{
            url:      mediaURL,
            headers:  headers, // Inject the headers here
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

// getDashMediaRequests generates the full list of media segments for a DASH representation group.
func getDashMediaRequests(group []*dash.Representation, sidxData []byte) ([]segment, error) {
   if len(group) == 0 {
      return nil, nil
   }
   if group[0].SegmentBase != nil {
      return generateSegmentsFromSidx(group[0], sidxData, true)
   }
   var requests []segment
   for _, rep := range group {
      segs, err := generateSegments(rep)
      if err != nil {
         return nil, err
      }
      requests = append(requests, segs...)
   }
   return requests, nil
}

// dash.go

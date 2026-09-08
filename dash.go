// dash.go
package maya

import (
   "41.neocities.org/luna/dash"
   "fmt"
   "slices"
)

// downloadDash parses a DASH manifest, extracts all necessary data, and passes it to the central orchestrator.
func downloadDash(mpd *dash.Mpd, streamId string, fetchKey keyFetcher) error {
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
   protection, err := getDashProtection(rep)
   if err != nil {
      return err
   }
   job := &downloadJob{
      outputFileNameBase: rep.Id,
      info:               info,
      manifestProtection: protection,
      fetchKey:           fetchKey,
   }

   // SegmentBase: one URL, one request handed straight to sofia.Process,
   // which reads the moov from the stream itself. No sidx. The init
   // segment is fetched on demand only when resuming, whose byte offset
   // is past the moov at the front of the stream.
   if rep.SegmentBase != nil {
      baseUrl, err := rep.ResolveBaseUrl()
      if err != nil {
         return err
      }
      job.single = &segment{url: baseUrl}
      job.fetchInit = func() ([]byte, error) {
         return getDashInitSegment(rep, info)
      }
      return orchestrateDownload(job)
   }

   initData, err := getDashInitSegment(rep, info)
   if err != nil {
      return err
   }
   job.initSegmentData = initData
   allRequests, err := getDashMediaRequests(dashGroup)
   if err != nil {
      return err
   }
   job.allRequests = allRequests
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
func getDashMediaRequests(group []*dash.Representation) ([]segment, error) {
   if len(group) == 0 {
      return nil, nil
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

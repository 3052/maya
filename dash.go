// dash.go
package maya

import (
   "41.neocities.org/luna/dash"
   "errors"
   "fmt"
   "log"
   "net/http"
   "net/url"
   "slices"
)

// getMiddleBitrate calculates an accurate bitrate for a representation and stores it...
func getMiddleBitrate(rep *dash.Representation) error {
   log.Println("update", rep.Id)
   if rep.SegmentBase != nil {
      baseUrl, err := rep.ResolveBaseUrl()
      if err != nil {
         return err
      }

      header := http.Header{}
      header.Set("range", "bytes="+rep.SegmentBase.IndexRange)
      sidxData, err := getSegment(baseUrl, header)
      if err != nil {
         return err
      }

      segs, err := generateSegmentsFromSidx(rep, sidxData, false)
      if err != nil {
         return err
      }
      if len(segs) == 0 {
         return nil
      }
      var (
         totalSizeBits uint64
         totalDuration float64
      )
      for _, seg := range segs {
         totalSizeBits += seg.sizeBits
         totalDuration += seg.duration
      }
      if totalDuration <= 0 {
         return errors.New("invalid total duration from sidx for bitrate calculation")
      }
      rep.MedianBandwidth = int(float64(totalSizeBits) / totalDuration)
      return nil
   }
   segs, err := generateSegments(rep)
   if err != nil {
      return err
   }
   if len(segs) == 0 {
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
   rep.MedianBandwidth = int(float64(sizeBits) / mid.duration)
   return nil
}

// getDashInitSegment locates and fetches the initialization segment for a DASH representation.
func getDashInitSegment(rep *dash.Representation, typeInfo *typeInfo) ([]byte, error) {
   if !typeInfo.IsFMP4 {
      return nil, nil
   }
   // Case 1: Initialization defined in SegmentBase
   if rep.SegmentBase != nil && rep.SegmentBase.Initialization != nil {
      baseUrl, err := rep.ResolveBaseUrl()
      if err != nil {
         return nil, err
      }
      header := http.Header{}
      header.Set("range", "bytes="+rep.SegmentBase.Initialization.Range)
      return getSegment(baseUrl, header)
   }
   // Case 2: Initialization defined in SegmentTemplate
   if template := rep.GetSegmentTemplate(); template != nil && template.Initialization != "" {
      initUrl, err := template.ResolveInitialization(rep)
      if err != nil {
         return nil, fmt.Errorf("failed to resolve DASH SegmentTemplate initialization URL: %w", err)
      }
      return getSegment(initUrl, nil)
   }
   // Case 3: Initialization defined in SegmentList
   if sl := rep.SegmentList; sl != nil && sl.Initialization != nil {
      initUrl, err := sl.Initialization.ResolveSourceUrl()
      if err != nil {
         return nil, fmt.Errorf("failed to resolve DASH SegmentList initialization URL: %w", err)
      }
      return getSegment(initUrl, nil)
   }
   return nil, nil
}

// downloadDash parses a DASH manifest, extracts all necessary data, and passes it to the central orchestrator.
func downloadDash(manifest *dash.Mpd, threads int, streamId string, fetchKey keyFetcher) error {
   dashGroup, ok := manifest.GetRepresentations()[streamId]
   if !ok {
      return fmt.Errorf("representation group not found %v", streamId)
   }
   if len(dashGroup) == 0 {
      return fmt.Errorf("representation group is empty")
   }
   rep := dashGroup[0]
   typeInfo, err := detectDashType(rep)
   if err != nil {
      return err
   }
   var sidxData []byte
   if rep.SegmentBase != nil {
      baseUrl, err := rep.ResolveBaseUrl()
      if err != nil {
         return err
      }
      header := http.Header{}
      header.Set("range", "bytes="+rep.SegmentBase.IndexRange)
      sidxData, err = getSegment(baseUrl, header)
      if err != nil {
         return fmt.Errorf("failed to pre-fetch sidx data: %w", err)
      }
   }
   allRequests, err := getDashMediaRequests(dashGroup, sidxData)
   if err != nil {
      return err
   }
   initData, err := getDashInitSegment(rep, typeInfo)
   if err != nil {
      return err
   }
   protection, err := getDashProtection(rep)
   if err != nil {
      return err
   }
   job := &downloadJob{
      outputFileNameBase: rep.Id,
      typeInfo:           typeInfo,
      allRequests:        allRequests,
      initSegmentData:    initData,
      manifestProtection: protection,
      threads:            threads,
      fetchKey:           fetchKey,
   }
   return orchestrateDownload(job)
}

// detectDashType determines the file extension and container type from a DASH Representation's metadata.
func detectDashType(rep *dash.Representation) (*typeInfo, error) {
   switch rep.GetMimeType() {
   case "video/mp4":
      return &typeInfo{Extension: ".mp4", IsFMP4: true}, nil
   case "audio/mp4":
      return &typeInfo{Extension: ".m4a", IsFMP4: true}, nil
   case "text/vtt":
      return &typeInfo{Extension: ".vtt", IsFMP4: false}, nil
   default:
      return nil, fmt.Errorf("unsupported mime type for stream %s: %s", rep.Id, rep.GetMimeType())
   }
}

// listStreamsDash is an internal helper to print streams from a parsed manifest
func listStreamsDash(manifest *dash.Mpd) error {
   groups := manifest.GetRepresentations()
   repsForSorting := make([]*dash.Representation, 0, len(groups))
   for _, group := range groups {
      representation := group[len(group)/2]
      if representation.GetMimeType() == "video/mp4" {
         if err := getMiddleBitrate(representation); err != nil {
            return fmt.Errorf("could not calculate bitrate for stream %s: %w", representation.Id, err)
         }
      }
      repsForSorting = append(repsForSorting, representation)
   }
   slices.SortFunc(repsForSorting, dash.Bandwidth)
   for i, representation := range repsForSorting {
      if i > 0 {
         fmt.Println()
      }
      fmt.Println(representation)
   }
   return nil
}

// parseDash is an internal helper to parse a DASH manifest.
func parseDash(body []byte, baseURL *url.URL) (*dash.Mpd, error) {
   manifest, err := dash.Parse(body)
   if err != nil {
      return nil, fmt.Errorf("failed to parse DASH manifest: %w", err)
   }
   manifest.MpdUrl = baseURL
   return manifest, nil
}

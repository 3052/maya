package maya

import (
   "41.neocities.org/drm/widevine"
   "41.neocities.org/luna/dash"
   "41.neocities.org/sofia"
   "errors"
   "fmt"
   "log"
   "net/http"
   "strings"
)

// getMiddleBitrate calculates an accurate bitrate for a representation and
// stores it in MedianBandwidth
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
         header.Set("range", "bytes="+rep.SegmentBase.IndexRange)
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

// getDashProtection extracts Widevine PSSH data from a representation.
func getDashProtection(rep *dash.Representation) (*protectionInfo, error) {
   const widevineURN = "urn:uuid:edef8ba9-79d6-4ace-a3c8-27dcd51d21ed"
   var psshData []byte
   for _, contentProtection := range rep.GetContentProtection() {
      if strings.ToLower(contentProtection.SchemeIdUri) == widevineURN {
         pssh, err := contentProtection.GetPssh()
         if err != nil {
            return nil, fmt.Errorf("could not parse widevine pssh from manifest: %w", err)
         }
         if pssh != nil {
            psshData = pssh
            break // Found it
         }
      }
   }

   if psshData == nil {
      return nil, nil
   }

   var psshBox sofia.PsshBox
   if err := psshBox.Parse(psshData); err != nil {
      return nil, fmt.Errorf("could not parse pssh box from dash manifest: %w", err)
   }

   var wvData widevine.PsshData
   if err := wvData.Unmarshal(psshBox.Data); err != nil {
      // Not a fatal error, might just be a PSSH without a content ID
      return &protectionInfo{ContentId: nil, KeyId: nil}, nil
   }

   // The KeyId field is explicitly set to nil, as it must only come from the MP4.
   return &protectionInfo{ContentId: wvData.ContentId, KeyId: nil}, nil
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

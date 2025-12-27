package maya

import (
   "41.neocities.org/luna/dash"
   "41.neocities.org/luna/hls"
   "fmt"
   "log"
   "net/http"
   "net/url"
   "os"
   "strconv"
)

// downloadDASHInternal orchestrates the entire download process for a DASH stream.
func downloadDASHInternal(c *Config, manifest *dash.Mpd, drmCfg *drmConfig) error {
   dashGroup, ok := manifest.GetRepresentations()[c.StreamId]
   if !ok {
      return fmt.Errorf("representation group not found %v", c.StreamId)
   }
   if len(dashGroup) == 0 {
      return fmt.Errorf("representation group is empty")
   }
   rep := dashGroup[0]

   // Step 1: Pre-fetch sidx data if needed.
   var sidxData []byte
   var err error
   if rep.SegmentBase != nil {
      baseUrl, err_base := rep.ResolveBaseUrl()
      if err_base != nil {
         return err_base
      }
      header := http.Header{}
      header.Set("Range", "bytes="+rep.SegmentBase.IndexRange)
      sidxData, err = getSegment(baseUrl, header)
      if err != nil {
         return err
      }
   }

   // Step 2: Get the first segment's data for content detection.
   var firstData []byte
   isInitSegmentBased := rep.SegmentBase != nil && rep.SegmentBase.Initialization != nil
   if isInitSegmentBased {
      baseUrl, _ := rep.ResolveBaseUrl()
      header := http.Header{"Range": []string{"bytes=" + rep.SegmentBase.Initialization.Range}}
      firstData, err = getSegment(baseUrl, header)
   } else {
      var segs []segment
      if rep.SegmentBase != nil {
         segs, err = generateSegmentsFromSidx(rep, sidxData)
      } else {
         segs, err = generateSegments(rep)
      }
      if err != nil {
         return err
      }
      if len(segs) == 0 {
         return nil
      }
      firstData, err = getSegment(segs[0].url, segs[0].header)
   }
   if err != nil {
      return fmt.Errorf("failed to download first segment for content detection: %w", err)
   }

   // Step 3: Detect type, create file, and set up DRM.
   detection := detectContentType(firstData)
   if detection.Extension == "" {
      return fmt.Errorf("could not determine file type for stream %s", rep.Id)
   }
   fileName := rep.Id + detection.Extension
   log.Println("Create", fileName)
   file, err := os.Create(fileName)
   if err != nil {
      return err
   }
   defer file.Close()

   var media mediaFile
   if drmCfg != nil {
      protection, _ := getDashProtection(rep, drmCfg.Scheme)
      if protection != nil {
         if err := media.configureProtection(protection); err != nil {
            return err
         }
      }
   }

   // Step 4: Prepare remuxer and get decryption key.
   remux, err := initializeRemuxer(detection.IsFMP4, file, firstData, &media)
   if err != nil {
      return err
   }
   key, err := c.fetchKey(drmCfg, &media)
   if err != nil {
      return err
   }

   // Step 5: Get all media requests and execute the download.
   requests, err := getDashMediaRequests(dashGroup, sidxData)
   if err != nil {
      return err
   }

   remainingRequests := requests
   if len(requests) > 0 && !isInitSegmentBased {
      remainingRequests = requests[1:]
   }
   return c.executeDownload(remainingRequests, key, remux, file)
}

// downloadHLSInternal orchestrates the entire download process for an HLS stream.
func downloadHLSInternal(c *Config, playlist *hls.MasterPlaylist, drmCfg *drmConfig) error {
   keyInt, err := strconv.Atoi(c.StreamId)
   if err != nil {
      return fmt.Errorf("invalid HLS variant StreamId, must be an integer: %q", c.StreamId)
   }
   baseURL := playlist.Variants[0].URI

   // Find the target stream and get its metadata.
   var targetURI *url.URL
   var targetID string
   var mediaPl *hls.MediaPlaylist
   for _, v := range playlist.Variants {
      if v.ID == keyInt {
         targetURI, targetID = v.URI, strconv.Itoa(v.ID)
         break
      }
   }
   if targetURI == nil {
      for _, r := range playlist.Medias {
         if r.ID == keyInt {
            targetURI, targetID = r.URI, strconv.Itoa(r.ID)
            break
         }
      }
   }
   if targetURI == nil {
      return fmt.Errorf("stream with ID not found: %d", keyInt)
   }

   mediaPl, err = fetchMediaPlaylist(targetURI, baseURL)
   if err != nil {
      return err
   }
   segments, err := hlsSegments(mediaPl)
   if err != nil || len(segments) == 0 {
      return err
   }

   // Step 2: Download first segment, detect, create file.
   firstData, err := getSegment(segments[0].url, segments[0].header)
   if err != nil {
      return err
   }
   detection := detectContentType(firstData)
   if detection.Extension == "" {
      return fmt.Errorf("could not determine file type for stream %s", targetID)
   }
   fileName := targetID + detection.Extension
   log.Println("Create", fileName)
   file, err := os.Create(fileName)
   if err != nil {
      return err
   }
   defer file.Close()

   // Step 3: Configure DRM.
   var media mediaFile
   if drmCfg != nil {
      protection, _ := getHlsProtection(mediaPl, drmCfg.Scheme)
      if protection != nil {
         if err := media.configureProtection(protection); err != nil {
            return err
         }
      }
   }

   // Step 4: Prepare remuxer and get key.
   remux, err := initializeRemuxer(detection.IsFMP4, file, firstData, &media)
   if err != nil {
      return err
   }
   key, err := c.fetchKey(drmCfg, &media)
   if err != nil {
      return err
   }

   // Step 5: Execute download.
   requests := make([]mediaRequest, len(segments))
   for i, s := range segments {
      requests[i] = mediaRequest{url: s.url, header: s.header}
   }
   return c.executeDownload(requests[1:], key, remux, file)
}

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

// finishDownload takes the prepared, format-specific data and runs the shared
// part of the download process: content detection, file creation, DRM, remuxing,
// and execution.
func (c *Config) finishDownload(
   drmCfg *drmConfig,
   firstData []byte,
   streamID string,
   protection *protectionInfo,
   allRequests []mediaRequest,
   skipFirst bool,
) error {
   if firstData == nil {
      log.Println("Stream contains no data.")
      return nil
   }

   // Step 1: Detect type and create file.
   detection := detectContentType(firstData)
   if detection.Extension == "" {
      return fmt.Errorf("could not determine file type for stream %s", streamID)
   }
   fileName := streamID + detection.Extension
   log.Println("Create", fileName)
   file, err := os.Create(fileName)
   if err != nil {
      return err
   }
   defer file.Close()

   // Step 2: Configure DRM.
   var media mediaFile
   if drmCfg != nil && protection != nil {
      if err := media.configureProtection(protection); err != nil {
         return err
      }
   }

   // Step 3: Prepare remuxer and get decryption key.
   remux, err := initializeRemuxer(detection.IsFMP4, file, firstData, &media)
   if err != nil {
      return err
   }
   key, err := c.fetchKey(drmCfg, &media)
   if err != nil {
      return err
   }

   // Step 4: Execute the download.
   remainingRequests := allRequests
   if skipFirst && len(allRequests) > 0 {
      remainingRequests = allRequests[1:]
   }

   return c.executeDownload(remainingRequests, key, remux, file)
}

// downloadDashInternal prepares all DASH-specific data and passes it to finishDownload.
func (c *Config) downloadDashInternal(manifest *dash.Mpd, drmCfg *drmConfig) error {
   dashGroup, ok := manifest.GetRepresentations()[c.StreamId]
   if !ok {
      return fmt.Errorf("representation group not found %v", c.StreamId)
   }
   if len(dashGroup) == 0 {
      return fmt.Errorf("representation group is empty")
   }
   rep := dashGroup[0]

   // 1. Prepare DASH-specific info
   var sidxData []byte
   var err error
   if rep.SegmentBase != nil {
      var baseUrl *url.URL
      baseUrl, err = rep.ResolveBaseUrl()
      if err != nil {
         return err
      }
      header := http.Header{}
      header.Set("Range", "bytes="+rep.SegmentBase.IndexRange)
      sidxData, err = getSegment(baseUrl, header)
      if err != nil {
         return fmt.Errorf("failed to pre-fetch sidx data: %w", err)
      }
   }

   // 2. Prepare inputs for the shared downloader
   var firstData []byte
   isInitSegmentBased := rep.SegmentBase != nil && rep.SegmentBase.Initialization != nil
   if isInitSegmentBased {
      baseUrl, _ := rep.ResolveBaseUrl()
      header := http.Header{"Range": []string{"bytes=" + rep.SegmentBase.Initialization.Range}}
      firstData, err = getSegment(baseUrl, header)
   } else {
      segs, segsErr := getDashMediaRequests(dashGroup, sidxData)
      if segsErr != nil {
         return segsErr
      }
      if len(segs) > 0 {
         firstData, err = getSegment(segs[0].url, segs[0].header)
      }
   }
   if err != nil {
      return fmt.Errorf("failed to get initial DASH data: %w", err)
   }

   allRequests, err := getDashMediaRequests(dashGroup, sidxData)
   if err != nil {
      return err
   }

   protection, _ := getDashProtection(rep)
   shouldSkip := !isInitSegmentBased

   // 3. Call the shared downloader
   return c.finishDownload(drmCfg, firstData, rep.Id, protection, allRequests, shouldSkip)
}

// downloadHlsInternal prepares all HLS-specific data and passes it to finishDownload.
func (c *Config) downloadHlsInternal(playlist *hls.MasterPlaylist, drmCfg *drmConfig) error {
   keyInt, err := strconv.Atoi(c.StreamId)
   if err != nil {
      return fmt.Errorf("invalid HLS variant StreamId, must be an integer: %q", c.StreamId)
   }

   // 1. Prepare HLS-specific info
   var targetURI *url.URL
   var targetID string
   for _, v := range playlist.Streams {
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

   mediaPl, err := fetchMediaPlaylist(targetURI)
   if err != nil {
      return err
   }

   hlsSegs, err := hlsSegments(mediaPl)
   if err != nil {
      return err
   }

   // 2. Prepare inputs for the shared downloader
   var firstData []byte
   if len(hlsSegs) > 0 {
      firstData, err = getSegment(hlsSegs[0].url, hlsSegs[0].header)
      if err != nil {
         return fmt.Errorf("failed to get initial HLS data: %w", err)
      }
   }

   allRequests := make([]mediaRequest, len(hlsSegs))
   for i, s := range hlsSegs {
      allRequests[i] = mediaRequest{url: s.url, header: s.header}
   }

   protection, _ := getHlsProtection(mediaPl)
   shouldSkip := true

   // 3. Call the shared downloader
   return c.finishDownload(drmCfg, firstData, targetID, protection, allRequests, shouldSkip)
}

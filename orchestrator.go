package maya

import (
   "41.neocities.org/drm/widevine"
   "41.neocities.org/luna/dash"
   "41.neocities.org/luna/hls"
   "41.neocities.org/sofia"
   "encoding/hex"
   "fmt"
   "log"
   "net/http"
   "net/url"
   "os"
   "strconv"
)

// keyFetcher is a function type that abstracts the DRM-specific key retrieval process.
type keyFetcher func(keyID, contentID []byte) ([]byte, error)

// runDownload is the central, shared entry point for all job types.
func runDownload(
   manifest *dash.Mpd,
   playlist *hls.MasterPlaylist,
   threads int,
   streamId string,
   fetchKey keyFetcher,
) error {
   if manifest != nil {
      return downloadDash(manifest, threads, streamId, fetchKey)
   }
   if playlist != nil {
      return downloadHls(playlist, threads, streamId, fetchKey)
   }
   return fmt.Errorf("no valid manifest or playlist provided")
}

// downloadDash prepares all DASH-specific data and passes it to the shared execution logic.
func downloadDash(manifest *dash.Mpd, threads int, streamId string, fetchKey keyFetcher) error {
   dashGroup, ok := manifest.GetRepresentations()[streamId]
   if !ok {
      return fmt.Errorf("representation group not found %v", streamId)
   }
   if len(dashGroup) == 0 {
      return fmt.Errorf("representation group is empty")
   }
   rep := dashGroup[0]
   // 1. Prepare DASH-specific info
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
   // 2. Prepare inputs for the shared downloader
   var firstData []byte
   isInitSegmentBased := rep.SegmentBase != nil && rep.SegmentBase.Initialization != nil
   if isInitSegmentBased {
      baseUrl, err := rep.ResolveBaseUrl()
      if err != nil {
         return err
      }
      header := http.Header{"Range": []string{"bytes=" + rep.SegmentBase.Initialization.Range}}
      firstData, err = getSegment(baseUrl, header)
      if err != nil {
         return err
      }
   } else {
      segs, err := getDashMediaRequests(dashGroup, sidxData)
      if err != nil {
         return err
      }
      if len(segs) > 0 {
         firstData, err = getSegment(segs[0].url, segs[0].header)
         if err != nil {
            return err
         }
      }
   }
   allRequests, err := getDashMediaRequests(dashGroup, sidxData)
   if err != nil {
      return err
   }
   protection, err := getDashProtection(rep)
   if err != nil {
      return err
   }
   shouldSkip := !isInitSegmentBased
   // 3. Call the shared execution logic
   return execute(firstData, rep.Id, protection, allRequests, shouldSkip, threads, fetchKey)
}

// downloadHls prepares all HLS-specific data and passes it to the shared execution logic.
func downloadHls(playlist *hls.MasterPlaylist, threads int, streamId string, fetchKey keyFetcher) error {
   keyInt, err := strconv.Atoi(streamId)
   if err != nil {
      return fmt.Errorf("invalid HLS variant StreamId, must be an integer: %q", streamId)
   }
   // 1. Prepare HLS-specific info
   var targetURI *url.URL
   for _, variant := range playlist.Streams {
      if variant.ID == keyInt {
         targetURI = variant.URI
         break
      }
   }
   if targetURI == nil {
      for _, rendition := range playlist.Medias {
         if rendition.ID == keyInt {
            targetURI = rendition.URI
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
   for i, segment := range hlsSegs {
      allRequests[i] = mediaRequest{url: segment.url, header: segment.header}
   }
   protection, err := getHlsProtection(mediaPl)
   if err != nil {
      return err
   }
   shouldSkip := true
   // 3. Call the shared execution logic
   return execute(firstData, streamId, protection, allRequests, shouldSkip, threads, fetchKey)
}

// execute takes the prepared, format-specific data and runs the download.
func execute(
   firstData []byte,
   streamID string,
   manifestProtection *protectionInfo,
   allRequests []mediaRequest,
   skipFirst bool,
   threads int,
   fetchKey keyFetcher,
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
   // Step 2: Prepare remuxer and get DRM info from init segment.
   remux, initProtection, err := initializeRemuxer(detection.IsFMP4, file, firstData)
   if err != nil {
      return err
   }
   // Step 3: Assemble final DRM info and fetch key.
   var key []byte
   if fetchKey != nil {
      var keyID, contentID []byte
      // Start with info from the manifest (DASH/HLS).
      if manifestProtection != nil {
         keyID = manifestProtection.KeyID
         if len(manifestProtection.Pssh) > 0 {
            var wvData widevine.PsshData
            psshBox := sofia.PsshBox{}
            if err := psshBox.Parse(manifestProtection.Pssh); err == nil {
               if err := wvData.Unmarshal(psshBox.Data); err == nil {
                  contentID = wvData.ContentId
               }
            }
         }
      }
      // The init segment can also contain a PSSH box with a KeyID.
      if keyID == nil && initProtection != nil {
         keyID = initProtection.KeyID
         log.Printf("key ID from PSSH: %x", keyID)
      }
      if keyID == nil {
         return fmt.Errorf("no key ID found for protected stream")
      }
      key, err = fetchKey(keyID, contentID)
      if err != nil {
         return fmt.Errorf("failed to fetch decryption key: %w", err)
      }
   }
   // Step 4: Execute the download using the worker pool engine.
   remainingRequests := allRequests
   if skipFirst && len(allRequests) > 0 {
      remainingRequests = allRequests[1:]
   }
   return executeDownload(remainingRequests, key, remux, file, threads)
}

func initializeRemuxer(isFMP4 bool, file *os.File, firstData []byte) (*sofia.Remuxer, *protectionInfo, error) {
   if !isFMP4 {
      if _, err := file.Write(firstData); err != nil {
         return nil, nil, err
      }
      return nil, nil, nil
   }
   var remux sofia.Remuxer
   remux.Writer = file
   if len(firstData) == 0 {
      return &remux, nil, nil
   }
   if err := remux.Initialize(firstData); err != nil {
      return nil, nil, err
   }
   var initProtection *protectionInfo
   wvIDBytes, err := hex.DecodeString(widevineSystemId)
   if err != nil {
      panic("failed to decode hardcoded widevine system id")
   }
   if wvBox, ok := remux.Moov.FindPssh(wvIDBytes); ok {
      var psshData widevine.PsshData
      if err := psshData.Unmarshal(wvBox.Data); err == nil {
         if len(psshData.KeyIds) > 0 {
            initProtection = &protectionInfo{KeyID: psshData.KeyIds[0]}
         }
      }
   }
   remux.Moov.RemovePssh()
   return &remux, initProtection, nil
}

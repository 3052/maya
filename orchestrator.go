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
)

// manifestType is an enum to distinguish between DASH and HLS.
type manifestType int

const (
   dashManifest manifestType = iota
   hlsManifest
)

// keyFetcher is a function type that abstracts the DRM-specific key retrieval process.
type keyFetcher func(keyId, contentId []byte) ([]byte, error)

// runDownload is the central, shared entry point that orchestrates the entire download.
func runDownload(
   body []byte,
   baseURL *url.URL,
   threads int,
   streamId string,
   mType manifestType,
   fetchKey keyFetcher,
) error {
   if mType == dashManifest {
      manifest, err := parseDash(body, baseURL)
      if err != nil {
         return err
      }
      return downloadDash(manifest, threads, streamId, fetchKey)
   }
   playlist, err := parseHls(body, baseURL)
   if err != nil {
      if mType == hlsManifest {
         return err
      }
      manifest, err2 := parseDash(body, baseURL)
      if err2 != nil {
         return fmt.Errorf("failed to parse manifest as HLS or DASH")
      }
      return downloadDash(manifest, threads, streamId, fetchKey)
   }
   return downloadHls(playlist, threads, streamId, fetchKey)
}

// downloadDash prepares all DASH-specific data and passes it to the engine.
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
   fileName := rep.Id + typeInfo.Extension
   log.Println("Create", fileName)
   file, err := os.Create(fileName)
   if err != nil {
      return err
   }
   defer file.Close()
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
   if !typeInfo.IsFMP4 {
      // Non-FMP4 streams (e.g., VTT): download all segments and concatenate them directly.
      // No remuxer or initialization segment is needed.
      return executeDownload(allRequests, nil, nil, file, threads)
   }
   // FMP4 streams: require an initialization segment and a remuxer.
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
   } else if template := rep.GetSegmentTemplate(); template != nil && template.Initialization != "" {
      initUrl, err := template.ResolveInitialization(rep)
      if err != nil {
         return fmt.Errorf("failed to resolve DASH initialization URL: %w", err)
      }
      firstData, err = getSegment(initUrl, nil)
      if err != nil {
         return fmt.Errorf("failed to get DASH initialization segment: %w", err)
      }
   }
   remux, initProtection, err := initializeRemuxer(true, file, firstData)
   if err != nil {
      return err
   }
   protection, err := getDashProtection(rep)
   if err != nil {
      return err
   }
   var key []byte
   if fetchKey != nil {
      key, err = getKeyForStream(fetchKey, protection, initProtection)
      if err != nil {
         return err
      }
   }
   return executeDownload(allRequests, key, remux, file, threads)
}

// downloadHls prepares all HLS-specific data and passes it to the engine.
func downloadHls(playlist *hls.MasterPlaylist, threads int, streamId string, fetchKey keyFetcher) error {
   typeInfo, targetURI, err := detectHlsType(playlist, streamId)
   if err != nil {
      return err
   }
   fileName := streamId + typeInfo.Extension
   log.Println("Create", fileName)
   file, err := os.Create(fileName)
   if err != nil {
      return err
   }
   defer file.Close()
   mediaPl, err := fetchMediaPlaylist(targetURI)
   if err != nil {
      return err
   }
   hlsSegs, err := hlsSegments(mediaPl)
   if err != nil {
      return err
   }
   allRequests := make([]mediaRequest, len(hlsSegs))
   for i, seg := range hlsSegs {
      allRequests[i] = mediaRequest{url: seg.url, header: seg.header}
   }
   if !typeInfo.IsFMP4 {
      // Non-FMP4 streams: download all segments and concatenate them directly.
      return executeDownload(allRequests, nil, nil, file, threads)
   }
   // FMP4 streams: require an initialization segment and a remuxer.
   var firstData []byte
   if mediaPl.Map != nil {
      firstData, err = getSegment(mediaPl.Map, nil)
      if err != nil {
         return fmt.Errorf("failed to get HLS initialization segment: %w", err)
      }
   }
   remux, initProtection, err := initializeRemuxer(true, file, firstData)
   if err != nil {
      return err
   }
   protection, err := getHlsProtection(mediaPl)
   if err != nil {
      return err
   }
   var key []byte
   if fetchKey != nil {
      key, err = getKeyForStream(fetchKey, protection, initProtection)
      if err != nil {
         return err
      }
   }
   return executeDownload(allRequests, key, remux, file, threads)
}

// getKeyForStream determines the correct key ID to use and fetches the key.
func getKeyForStream(fetchKey keyFetcher, manifestProtection, initProtection *protectionInfo) ([]byte, error) {
   var keyId, contentId []byte
   if manifestProtection != nil {
      keyId = manifestProtection.KeyId
      if len(manifestProtection.Pssh) > 0 {
         var wvData widevine.PsshData
         psshBox := sofia.PsshBox{}
         if err := psshBox.Parse(manifestProtection.Pssh); err == nil {
            if err := wvData.Unmarshal(psshBox.Data); err == nil {
               contentId = wvData.ContentId
            }
         }
      }
   }
   if keyId == nil && initProtection != nil {
      keyId = initProtection.KeyId
      log.Printf("key ID from PSSH: %x", keyId)
   }
   if keyId == nil {
      return nil, fmt.Errorf("no key ID found for protected stream")
   }
   key, err := fetchKey(keyId, contentId)
   if err != nil {
      return nil, fmt.Errorf("failed to fetch decryption key: %w", err)
   }
   return key, nil
}

func initializeRemuxer(isFMP4 bool, file *os.File, firstData []byte) (*sofia.Remuxer, *protectionInfo, error) {
   if !isFMP4 {
      return nil, nil, nil
   }
   var remux sofia.Remuxer
   remux.Writer = file
   if len(firstData) > 0 {
      if err := remux.Initialize(firstData); err != nil {
         return nil, nil, err
      }
   }
   var initProtection *protectionInfo
   wvIdBytes, err := hex.DecodeString(widevineSystemId)
   if err != nil {
      panic("failed to decode hardcoded widevine system id")
   }
   if remux.Moov != nil {
      if wvBox, ok := remux.Moov.FindPssh(wvIdBytes); ok {
         var psshData widevine.PsshData
         if err := psshData.Unmarshal(wvBox.Data); err == nil {
            if len(psshData.KeyIds) > 0 {
               initProtection = &protectionInfo{KeyId: psshData.KeyIds[0]}
            }
         }
      }
      remux.Moov.RemovePssh()
   }
   return &remux, initProtection, nil
}

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
   // Use the first representation in the group as a template for common properties.
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
   // Generate the full list of requests from all periods in the group.
   allRequests, err := getDashMediaRequests(dashGroup, sidxData)
   if err != nil {
      return err
   }
   var firstData []byte
   var skipFirst bool
   // Correctly locate and fetch the initialization segment.
   if typeInfo.IsFMP4 {
      isInitSegmentBased := rep.SegmentBase != nil && rep.SegmentBase.Initialization != nil
      if isInitSegmentBased {
         // Init segment is defined by a byte range in SegmentBase.
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
         // Init segment is defined by a URL template in SegmentTemplate.
         initUrl, err := template.ResolveInitialization(rep)
         if err != nil {
            return fmt.Errorf("failed to resolve DASH initialization URL: %w", err)
         }
         firstData, err = getSegment(initUrl, nil)
         if err != nil {
            return fmt.Errorf("failed to get DASH initialization segment: %w", err)
         }
      }
   } else if len(allRequests) > 0 {
      // For non-FMP4, the "first data" is just the first segment.
      firstData, err = getSegment(allRequests[0].url, allRequests[0].header)
      if err != nil {
         return err
      }
      skipFirst = true
   }
   protection, err := getDashProtection(rep)
   if err != nil {
      return err
   }
   return execute(firstData, rep.Id, typeInfo, protection, allRequests, skipFirst, threads, fetchKey)
}

// downloadHls prepares all HLS-specific data and passes it to the engine.
func downloadHls(playlist *hls.MasterPlaylist, threads int, streamId string, fetchKey keyFetcher) error {
   typeInfo, targetURI, err := detectHlsType(playlist, streamId)
   if err != nil {
      return err
   }
   mediaPl, err := fetchMediaPlaylist(targetURI)
   if err != nil {
      return err
   }
   hlsSegs, err := hlsSegments(mediaPl)
   if err != nil {
      return err
   }
   var firstData []byte
   var skipFirst bool
   // Correctly handle the initialization segment (EXT-X-MAP).
   if typeInfo.IsFMP4 && mediaPl.Map != nil {
      // FMP4 with a separate init segment. This is our firstData.
      firstData, err = getSegment(mediaPl.Map, nil)
      if err != nil {
         return fmt.Errorf("failed to get HLS initialization segment: %w", err)
      }
      // The worker pool should download all segments from the list.
      skipFirst = false
   } else if len(hlsSegs) > 0 {
      // No separate init segment. Use the first media segment as firstData.
      firstData, err = getSegment(hlsSegs[0].url, hlsSegs[0].header)
      if err != nil {
         return fmt.Errorf("failed to get initial HLS data: %w", err)
      }
      // The worker pool must skip this first segment since we've already handled it.
      skipFirst = true
   }
   allRequests := make([]mediaRequest, len(hlsSegs))
   for i, segment := range hlsSegs {
      allRequests[i] = mediaRequest{url: segment.url, header: segment.header}
   }
   protection, err := getHlsProtection(mediaPl)
   if err != nil {
      return err
   }
   return execute(firstData, streamId, typeInfo, protection, allRequests, skipFirst, threads, fetchKey)
}

// execute takes the prepared data, fetches keys, and starts the download engine.
func execute(
   firstData []byte,
   streamId string,
   typeInfo *typeInfo,
   manifestProtection *protectionInfo,
   allRequests []mediaRequest,
   skipFirst bool,
   threads int,
   fetchKey keyFetcher,
) error {
   if len(allRequests) == 0 && firstData == nil {
      log.Println("Stream contains no data.")
      return nil
   }
   fileName := streamId + typeInfo.Extension
   log.Println("Create", fileName)
   file, err := os.Create(fileName)
   if err != nil {
      return err
   }
   defer file.Close()
   remux, initProtection, err := initializeRemuxer(typeInfo.IsFMP4, file, firstData)
   if err != nil {
      return err
   }
   var key []byte
   if fetchKey != nil {
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
         return fmt.Errorf("no key ID found for protected stream")
      }
      key, err = fetchKey(keyId, contentId)
      if err != nil {
         return fmt.Errorf("failed to fetch decryption key: %w", err)
      }
   }
   remainingRequests := allRequests
   if skipFirst && len(allRequests) > 0 {
      remainingRequests = allRequests[1:]
   }
   return executeDownload(remainingRequests, key, remux, file, threads)
}

func initializeRemuxer(isFMP4 bool, file *os.File, firstData []byte) (*sofia.Remuxer, *protectionInfo, error) {
   if !isFMP4 {
      if firstData != nil {
         if _, err := file.Write(firstData); err != nil {
            return nil, nil, err
         }
      }
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

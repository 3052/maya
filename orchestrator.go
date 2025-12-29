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
type keyFetcher func(keyID, contentID []byte) ([]byte, error)

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

   typeInfo, err := DetectDashType(rep)
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

   var firstData []byte // This is the init segment for FMP4
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
   } else if !typeInfo.IsFMP4 { // For non-FMP4, get the first actual segment
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

   shouldSkip := !isInitSegmentBased && typeInfo.IsFMP4
   return execute(firstData, rep.Id, typeInfo, protection, allRequests, shouldSkip, threads, fetchKey)
}

// downloadHls prepares all HLS-specific data and passes it to the engine.
func downloadHls(playlist *hls.MasterPlaylist, threads int, streamId string, fetchKey keyFetcher) error {
   typeInfo, targetURI, err := DetectHlsType(playlist, streamId)
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
   if len(hlsSegs) > 0 {
      // For HLS, firstData is either the init segment (if EXT-X-MAP exists) or the first media segment.
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

   return execute(firstData, streamId, typeInfo, protection, allRequests, true, threads, fetchKey)
}

// execute takes the prepared data, fetches keys, and starts the download engine.
func execute(
   firstData []byte,
   streamID string,
   typeInfo *TypeInfo,
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

   fileName := streamID + typeInfo.Extension
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
      var keyID, contentID []byte
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
   wvIDBytes, err := hex.DecodeString(widevineSystemId)
   if err != nil {
      panic("failed to decode hardcoded widevine system id")
   }
   if remux.Moov != nil {
      if wvBox, ok := remux.Moov.FindPssh(wvIDBytes); ok {
         var psshData widevine.PsshData
         if err := psshData.Unmarshal(wvBox.Data); err == nil {
            if len(psshData.KeyIds) > 0 {
               initProtection = &protectionInfo{KeyID: psshData.KeyIds[0]}
            }
         }
      }
      remux.Moov.RemovePssh()
   }
   return &remux, initProtection, nil
}

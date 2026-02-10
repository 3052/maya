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
   "strings"
)

// orchestrateDownload contains the shared, high-level logic for executing any download job.
func orchestrateDownload(job *downloadJob) error {
   var name strings.Builder
   name.WriteString(strings.ReplaceAll(job.streamId, "/", "_"))
   name.WriteString(job.typeInfo.Extension)
   log.Println("Create", &name)
   file, err := os.Create(name.String())
   if err != nil {
      return err
   }
   defer file.Close()
   if !job.typeInfo.IsFMP4 {
      // Non-FMP4 streams (e.g., VTT): download all segments and concatenate them directly.
      return executeDownload(job.allRequests, nil, nil, file, job.threads)
   }
   // FMP4 streams: require an initialization segment and a remuxer.
   remux, initProtection, err := initializeRemuxer(true, file, job.initSegmentData)
   if err != nil {
      return err
   }
   var key []byte
   if job.fetchKey != nil {
      key, err = getKeyForStream(job.fetchKey, job.manifestProtection, initProtection)
      if err != nil {
         return err
      }
   }
   return executeDownload(job.allRequests, key, remux, file, job.threads)
}

// manifestType is an enum to distinguish between DASH and HLS.
type manifestType int

const (
   dashManifest manifestType = iota
   hlsManifest
)

// keyFetcher is a function type that abstracts the DRM-specific key retrieval process.
type keyFetcher func(keyId, contentId []byte) ([]byte, error)

// downloadJob holds all the extracted, manifest-agnostic information needed to run a download.
type downloadJob struct {
   streamId           string
   typeInfo           *typeInfo
   allRequests        []mediaRequest
   initSegmentData    []byte
   manifestProtection *protectionInfo
   threads            int
   fetchKey           keyFetcher
}

// runDownload is the main entry point that dispatches to the correct manifest-specific download logic.
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
   // Default to HLS if not DASH
   playlist, err := parseHls(body, baseURL)
   if err != nil {
      return err
   }
   return downloadHls(playlist, threads, streamId, fetchKey)
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
      streamId:           rep.Id,
      typeInfo:           typeInfo,
      allRequests:        allRequests,
      initSegmentData:    initData,
      manifestProtection: protection,
      threads:            threads,
      fetchKey:           fetchKey,
   }
   return orchestrateDownload(job)
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
      header := http.Header{"Range": []string{"bytes=" + rep.SegmentBase.Initialization.Range}}
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

// downloadHls parses an HLS manifest, extracts all necessary data, and passes it to the central orchestrator.
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
   allRequests := make([]mediaRequest, len(hlsSegs))
   for i, seg := range hlsSegs {
      allRequests[i] = mediaRequest{url: seg.url, header: seg.header}
   }
   var initData []byte
   if typeInfo.IsFMP4 && mediaPl.Map != nil {
      initData, err = getSegment(mediaPl.Map, nil)
      if err != nil {
         return fmt.Errorf("failed to get HLS initialization segment: %w", err)
      }
   }
   protection, err := getHlsProtection(mediaPl)
   if err != nil {
      return err
   }
   job := &downloadJob{
      streamId:           streamId,
      typeInfo:           typeInfo,
      allRequests:        allRequests,
      initSegmentData:    initData,
      manifestProtection: protection,
      threads:            threads,
      fetchKey:           fetchKey,
   }
   return orchestrateDownload(job)
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

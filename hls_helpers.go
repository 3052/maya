package maya

import (
   "41.neocities.org/drm/widevine"
   "41.neocities.org/luna/hls"
   "41.neocities.org/sofia"
   "fmt"
   "io"
   "net/http"
   "net/url"
   "strconv"
)

// widevineURN is the standard URN identifying the Widevine DRM system in manifests.
const widevineUrn = "urn:uuid:edef8ba9-79d6-4ace-a3c8-27dcd51d21ed"

// fetchMediaPlaylist fetches and parses an HLS media playlist.
func fetchMediaPlaylist(mediaURL *url.URL) (*hls.MediaPlaylist, error) {
   if mediaURL == nil {
      return nil, fmt.Errorf("HLS stream has no URI")
   }
   resp, err := http.Get(mediaURL.String())
   if err != nil {
      return nil, err
   }
   defer resp.Body.Close()
   body, err := io.ReadAll(resp.Body)
   if err != nil {
      return nil, err
   }
   mediaPl, err := hls.DecodeMedia(string(body))
   if err != nil {
      return nil, err
   }
   mediaPl.ResolveUris(mediaURL)
   return mediaPl, nil
}

// getHlsProtection extracts Widevine PSSH data from an HLS manifest.
func getHlsProtection(mediaPl *hls.MediaPlaylist) (*protectionInfo, error) {
   for _, key := range mediaPl.Keys {
      if key.KeyFormat == widevineUrn && key.Uri != nil && key.Uri.Scheme == "data" {
         psshData, err := key.DecodeData()
         if err != nil {
            return nil, fmt.Errorf("failed to decode Widevine PSSH data from HLS manifest: %w", err)
         }
         var psshBox sofia.PsshBox
         if err := psshBox.Parse(psshData); err != nil {
            return nil, fmt.Errorf("failed to parse pssh box from HLS manifest: %w", err)
         }
         var wvData widevine.PsshData
         if err := wvData.Unmarshal(psshBox.Data); err != nil {
            // Not fatal, continue in case there's another key tag
            continue
         }
         // ONLY return the Content ID. The KeyId field is explicitly set to nil for manifest data.
         return &protectionInfo{ContentId: wvData.ContentId, KeyId: nil}, nil
      }
   }
   return nil, nil // No Widevine PSSH data found.
}

// hlsSegments generates a list of segments from a media playlist.
func hlsSegments(mediaPl *hls.MediaPlaylist) ([]segment, error) {
   var segments []segment
   for _, hlsSeg := range mediaPl.Segments {
      segments = append(segments, segment{url: hlsSeg.Uri, header: nil})
   }
   return segments, nil
}

// downloadHls parses an HLS manifest, extracts all necessary data, and passes it to the central orchestrator.
func downloadHls(playlist *hls.MasterPlaylist, threads int, streamId int, fetchKey keyFetcher) error {
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
      outputFileNameBase: strconv.Itoa(streamId), // Use new field name
      typeInfo:           typeInfo,
      allRequests:        allRequests,
      initSegmentData:    initData,
      manifestProtection: protection,
      threads:            threads,
      fetchKey:           fetchKey,
   }
   return orchestrateDownload(job)
}

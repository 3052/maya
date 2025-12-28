package maya

import (
   "41.neocities.org/luna/hls"
   "fmt"
   "io"
   "net/http"
   "net/url"
)

const (
   // widevineURN is the standard URN identifying the Widevine DRM system in manifests.
   widevineURN = "urn:uuid:edef8ba9-79d6-4ace-a3c8-27dcd51d21ed"
)

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
   mediaPl.ResolveURIs(mediaURL)
   return mediaPl, nil
}

// getHlsProtection extracts the Widevine PSSH from an HLS manifest.
func getHlsProtection(mediaPl *hls.MediaPlaylist) (*protectionInfo, error) {
   for _, key := range mediaPl.Keys {
      if key.KeyFormat == widevineURN && key.URI != nil && key.URI.Scheme == "data" {
         psshData, err := key.DecodeData()
         if err != nil {
            return nil, fmt.Errorf("failed to decode Widevine PSSH data from HLS manifest: %w", err)
         }
         return &protectionInfo{Pssh: psshData}, nil
      }
   }
   return nil, nil // No Widevine PSSH data found.
}

// hlsSegments generates a list of segments from a media playlist.
func hlsSegments(mediaPl *hls.MediaPlaylist) ([]segment, error) {
   var segments []segment
   for _, hlsSeg := range mediaPl.Segments {
      segments = append(segments, segment{url: hlsSeg.URI, header: nil})
   }
   return segments, nil
}

package maya

import (
   "41.neocities.org/luna/hls"
   "fmt"
   "io"
   "log"
   "net/http"
   "net/url"
   "strings"
)

// fetchMediaPlaylist fetches and parses an HLS media playlist.
// It assumes the provided URI has already been resolved to an absolute URL.
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
   // URIs for segments *within* the media playlist are resolved relative to the
   // media playlist's own URL.
   mediaPl.ResolveURIs(mediaURL)
   return mediaPl, nil
}

// getHlsProtection extracts the Widevine PSSH from an HLS manifest.
// For CENC content, this PSSH contains the key ID needed for any DRM.
func getHlsProtection(mediaPl *hls.MediaPlaylist) (*protectionInfo, error) {
   for _, key := range mediaPl.Keys {
      keyFormat := strings.ToLower(key.KeyFormat)
      isWidevinePssh := strings.Contains(keyFormat, "widevine")

      if isWidevinePssh && key.URI != nil && key.URI.Scheme == "data" {
         psshData, err := key.DecodeData()
         if err != nil {
            log.Printf("failed to decode Widevine PSSH data from HLS manifest: %v", err)
            continue // Try the next key tag if this one is malformed
         }
         // The KeyID is inside the PSSH box and will be extracted later.
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

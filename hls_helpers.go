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

// fetchMediaPlaylist is a shared helper for both HLS variant and rendition streams.
func fetchMediaPlaylist(uri, base *url.URL) (*hls.MediaPlaylist, error) {
   if uri == nil {
      return nil, fmt.Errorf("HLS stream has no URI")
   }
   mediaURL := base.ResolveReference(uri)
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

// getHlsProtection finds the protection data that matches the requested scheme.
func getHlsProtection(mediaPl *hls.MediaPlaylist, scheme string) (*protectionInfo, error) {
   // Find the first EXT-X-KEY tag that matches the requested DRM scheme and has embedded PSSH data.
   for _, key := range mediaPl.Keys {
      keyFormat := strings.ToLower(key.KeyFormat)
      isWidevine := scheme == "widevine" && strings.Contains(keyFormat, "widevine")
      isPlayReady := scheme == "playready" && strings.Contains(keyFormat, "playready")

      if (isWidevine || isPlayReady) && key.URI != nil && key.URI.Scheme == "data" {
         psshData, err := key.DecodeData()
         if err != nil {
            log.Printf("failed to decode PSSH data from HLS manifest: %v", err)
            continue // Try next key
         }
         // HLS often puts the KID inside the PSSH box. We extract it later in the process.
         return &protectionInfo{Pssh: psshData}, nil
      }
   }
   return nil, nil // No matching protection data found.
}

// hlsSegments generates a list of segments from a media playlist.
func hlsSegments(mediaPl *hls.MediaPlaylist) ([]segment, error) {
   var segments []segment
   for _, hlsSeg := range mediaPl.Segments {
      segments = append(segments, segment{url: hlsSeg.URI, header: nil})
   }
   return segments, nil
}

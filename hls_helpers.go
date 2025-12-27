package maya

import (
   "41.neocities.org/luna/hls"
   "fmt"
   "io"
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
   if scheme == "widevine" && len(mediaPl.Keys) > 0 {
      hlsKey := mediaPl.Keys[0]
      if strings.Contains(hlsKey.KeyFormat, "widevine") && hlsKey.URI != nil && hlsKey.URI.Scheme == "data" {
         psshData, err := hlsKey.DecodeData()
         if err == nil {
            // HLS doesn't typically provide a KeyID in the same way as DASH,
            // so we leave it nil and rely on the PSSH.
            return &protectionInfo{Pssh: psshData}, nil
         }
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

package maya

import (
   "41.neocities.org/luna/dash"
   "41.neocities.org/luna/hls"
   "fmt"
   "log"
   "net/url"
)

// ParseDash parses a DASH manifest (MPD).
func ParseDash(body []byte, baseURL *url.URL) (*dash.Mpd, error) {
   manifest, err := dash.Parse(body)
   if err != nil {
      return nil, fmt.Errorf("failed to parse DASH manifest: %w", err)
   }
   manifest.MpdUrl = baseURL
   return manifest, nil
}

// ParseHls parses an HLS master or media playlist.
func ParseHls(body []byte, baseURL *url.URL) (*hls.MasterPlaylist, error) {
   bodyStr := string(body)
   master, err := hls.DecodeMaster(bodyStr)
   if err != nil {
      return nil, fmt.Errorf("failed to parse HLS playlist: %w", err)
   }
   master.ResolveURIs(baseURL)
   return master, nil
}

// --- Public Download Functions ---

// DownloadDash downloads a DASH stream.
// If c.Widevine or c.PlayReady is set, it will attempt decryption.
func (c *Config) DownloadDash(manifest *dash.Mpd) error {
   return c.downloadDashInternal(manifest)
}

// DownloadHls downloads an HLS stream.
// If c.Widevine or c.PlayReady is set, it will attempt decryption.
func (c *Config) DownloadHls(playlist *hls.MasterPlaylist) error {
   return c.downloadHlsInternal(playlist)
}

// --- List Functions ---

func ListStreamsDash(manifest *dash.Mpd) error {
   sidxCache := make(map[string][]byte)
   groups := manifest.GetRepresentations()
   // 1. Collect a representative from each group and calculate missing bitrates.
   repsForSorting := make([]*dash.Representation, 0, len(groups))
   for _, group := range groups {
      rep := group[len(group)/2]
      if rep.GetMimeType() == "video/mp4" {
         if err := getMiddleBitrate(rep, sidxCache); err != nil {
            log.Printf("Could not calculate bitrate for stream %s: %v", rep.Id, err)
         }
      }
      repsForSorting = append(repsForSorting, rep)
   }
   dash.SortByBandwidth(repsForSorting)
   // 3. Print the sorted list.
   for _, rep := range repsForSorting {
      fmt.Println(rep)
      fmt.Println()
   }
   return nil
}

func ListStreamsHls(playlist *hls.MasterPlaylist) error {
   playlist.Sort()
   for _, rendition := range playlist.Medias {
      fmt.Println(rendition)
      fmt.Println()
   }
   for _, variant := range playlist.Streams {
      fmt.Println(variant)
      fmt.Println()
   }
   return nil
}

// --- Config Struct ---

type Config struct {
   Send     func([]byte) ([]byte, error)
   Threads  int
   StreamId string

   // DRM configuration. Set only one of the following.
   Widevine  *WidevineConfig
   PlayReady *PlayReadyConfig
}

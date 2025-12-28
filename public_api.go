package maya

import (
   "41.neocities.org/luna/dash"
   "41.neocities.org/luna/hls"
   "fmt"
   "log"
   "net/url"
   "sort"
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
      // Fallback for media playlists presented as a master playlist
      if _, mediaErr := hls.DecodeMedia(bodyStr); mediaErr == nil {
         master = &hls.MasterPlaylist{Streams: []*hls.Stream{{URI: baseURL}}}
      } else {
         return nil, fmt.Errorf("failed to parse HLS playlist: %w", err)
      }
   }
   master.ResolveURIs(baseURL)
   return master, nil
}

// --- Public Download Functions ---

// DownloadDash downloads an unencrypted DASH stream.
func (c *Config) DownloadDash(manifest *dash.Mpd) error {
   return c.downloadDashInternal(manifest, nil)
}

// DownloadDashWidevine downloads a Widevine-encrypted DASH stream.
func (c *Config) DownloadDashWidevine(manifest *dash.Mpd, clientIDPath, privateKeyPath string) error {
   drmCfg := &drmConfig{
      ClientId:   clientIDPath,
      PrivateKey: privateKeyPath,
   }
   return c.downloadDashInternal(manifest, drmCfg)
}

// DownloadDashPlayReady downloads a PlayReady-encrypted DASH stream.
func (c *Config) DownloadDashPlayReady(manifest *dash.Mpd, certChainPath, encryptKeyPath string) error {
   drmCfg := &drmConfig{
      CertificateChain: certChainPath,
      EncryptSignKey:   encryptKeyPath,
   }
   return c.downloadDashInternal(manifest, drmCfg)
}

// DownloadHls downloads an unencrypted HLS stream.
func (c *Config) DownloadHls(playlist *hls.MasterPlaylist) error {
   return c.downloadHlsInternal(playlist, nil)
}

// DownloadHlsWidevine downloads a Widevine-encrypted HLS stream.
func (c *Config) DownloadHlsWidevine(playlist *hls.MasterPlaylist, clientIDPath, privateKeyPath string) error {
   drmCfg := &drmConfig{
      ClientId:   clientIDPath,
      PrivateKey: privateKeyPath,
   }
   return c.downloadHlsInternal(playlist, drmCfg)
}

// DownloadHlsPlayReady downloads a PlayReady-encrypted HLS stream.
func (c *Config) DownloadHlsPlayReady(playlist *hls.MasterPlaylist, certChainPath, encryptKeyPath string) error {
   drmCfg := &drmConfig{
      CertificateChain: certChainPath,
      EncryptSignKey:   encryptKeyPath,
   }
   return c.downloadHlsInternal(playlist, drmCfg)
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

   // 2. Sort the collected representations by bandwidth in ascending order.
   sort.Slice(repsForSorting, func(i, j int) bool {
      return repsForSorting[i].Bandwidth < repsForSorting[j].Bandwidth
   })

   // 3. Print the sorted list.
   for _, rep := range repsForSorting {
      fmt.Println(rep)
      fmt.Println()
   }

   return nil
}

func ListStreamsHls(playlist *hls.MasterPlaylist) error {
   for _, variant := range playlist.Streams {
      fmt.Println(variant)
      fmt.Println()
   }
   for _, rendition := range playlist.Medias {
      fmt.Println(rendition)
      fmt.Println()
   }
   return nil
}

// --- Config Struct ---

type Config struct {
   Send     func([]byte) ([]byte, error)
   Threads  int
   StreamId string
}

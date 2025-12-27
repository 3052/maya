package maya

import (
   "41.neocities.org/luna/dash"
   "41.neocities.org/luna/hls"
   "fmt"
   "log"
   "net/url"
)

// ParseDASH parses a DASH manifest (MPD).
func ParseDASH(body []byte, baseURL *url.URL) (*dash.Mpd, error) {
   manifest, err := dash.Parse(body)
   if err != nil {
      return nil, fmt.Errorf("failed to parse DASH manifest: %w", err)
   }
   manifest.MpdUrl = baseURL
   return manifest, nil
}

// ParseHLS parses an HLS master or media playlist.
func ParseHLS(body []byte, baseURL *url.URL) (*hls.MasterPlaylist, error) {
   bodyStr := string(body)
   master, err := hls.DecodeMaster(bodyStr)
   if err != nil {
      // Fallback for media playlists presented as a master playlist
      if _, mediaErr := hls.DecodeMedia(bodyStr); mediaErr == nil {
         master = &hls.MasterPlaylist{Variants: []*hls.Variant{{URI: baseURL}}}
      } else {
         return nil, fmt.Errorf("failed to parse HLS playlist: %w", err)
      }
   }
   master.ResolveURIs(baseURL)
   return master, nil
}

// --- Public Download Functions ---

// DownloadDASH downloads an unencrypted DASH stream.
func (c *Config) DownloadDASH(manifest *dash.Mpd) error {
   return downloadDASHInternal(c, manifest, nil)
}

// DownloadDASH_Widevine downloads a Widevine-encrypted DASH stream.
func (c *Config) DownloadDASH_Widevine(manifest *dash.Mpd, clientIDPath, privateKeyPath string) error {
   drmCfg := &drmConfig{
      Scheme:     "widevine",
      ClientId:   clientIDPath,
      PrivateKey: privateKeyPath,
   }
   return downloadDASHInternal(c, manifest, drmCfg)
}

// DownloadDASH_PlayReady downloads a PlayReady-encrypted DASH stream.
func (c *Config) DownloadDASH_PlayReady(manifest *dash.Mpd, certChainPath, encryptKeyPath string) error {
   drmCfg := &drmConfig{
      Scheme:           "playready",
      CertificateChain: certChainPath,
      EncryptSignKey:   encryptKeyPath,
   }
   return downloadDASHInternal(c, manifest, drmCfg)
}

// DownloadHLS downloads an unencrypted HLS stream.
func (c *Config) DownloadHLS(playlist *hls.MasterPlaylist) error {
   return downloadHLSInternal(c, playlist, nil)
}

// DownloadHLS_Widevine downloads a Widevine-encrypted HLS stream.
func (c *Config) DownloadHLS_Widevine(playlist *hls.MasterPlaylist, clientIDPath, privateKeyPath string) error {
   drmCfg := &drmConfig{
      Scheme:     "widevine",
      ClientId:   clientIDPath,
      PrivateKey: privateKeyPath,
   }
   return downloadHLSInternal(c, playlist, drmCfg)
}

// --- List Functions ---

func ListStreamsDASH(manifest *dash.Mpd) error {
   sidxCache := make(map[string][]byte)
   for _, group := range manifest.GetRepresentations() {
      rep := group[len(group)/2]
      if rep.GetMimeType() == "video/mp4" {
         if err := getMiddleBitrate(rep, sidxCache); err != nil {
            log.Printf("Could not calculate bitrate for stream %s: %v", rep.Id, err)
         }
      }
      fmt.Println(rep)
      fmt.Println()
   }
   return nil
}

func ListStreamsHLS(playlist *hls.MasterPlaylist) error {
   for _, variant := range playlist.Variants {
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

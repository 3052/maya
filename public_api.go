package maya

import (
   "41.neocities.org/luna/dash"
   "41.neocities.org/luna/hls"
   "flag"
   "fmt"
   "net/url"
)

func Usage(names ...string) {
   for _, name := range names {
      look := flag.Lookup(name)
      fmt.Printf("-%v %v\n", look.Name, look.Usage)
      if look.DefValue != "" {
         fmt.Printf("\tdefault %v\n", look.DefValue)
      }
   }
   fmt.Println()
}

// ListDash parses a DASH manifest and lists the available streams.
func ListDash(body []byte, baseURL *url.URL) error {
   manifest, err := parseDash(body, baseURL)
   if err != nil {
      return err
   }
   return listStreamsDash(manifest)
}

// ListHls parses an HLS playlist and lists the available streams.
func ListHls(body []byte, baseURL *url.URL) error {
   playlist, err := parseHls(body, baseURL)
   if err != nil {
      return err
   }
   return listStreamsHls(playlist)
}

// Job holds configuration for an unencrypted download.
type Job struct {
   Threads int
}

// DownloadDash parses and downloads a clear DASH stream.
func (j *Job) DownloadDash(body []byte, baseURL *url.URL, streamId string) error {
   return runDownload(body, baseURL, j.Threads, streamId, dashManifest, nil)
}

// DownloadHls parses and downloads a clear HLS stream.
func (j *Job) DownloadHls(body []byte, baseURL *url.URL, streamId string) error {
   return runDownload(body, baseURL, j.Threads, streamId, hlsManifest, nil)
}

// PlayReadyJob holds configuration for a PlayReady encrypted download.
type PlayReadyJob struct {
   Threads          int
   CertificateChain string
   EncryptSignKey   string
   Send             func([]byte) ([]byte, error)
}

// DownloadDash parses and downloads a PlayReady-encrypted DASH stream.
func (j *PlayReadyJob) DownloadDash(body []byte, baseURL *url.URL, streamId string) error {
   keyFetcher := func(keyId, contentId []byte) ([]byte, error) {
      return j.playReadyKey(keyId)
   }
   return runDownload(body, baseURL, j.Threads, streamId, dashManifest, keyFetcher)
}

// DownloadHls parses and downloads a PlayReady-encrypted HLS stream.
func (j *PlayReadyJob) DownloadHls(body []byte, baseURL *url.URL, streamId string) error {
   keyFetcher := func(keyId, contentId []byte) ([]byte, error) {
      return j.playReadyKey(keyId)
   }
   return runDownload(body, baseURL, j.Threads, streamId, hlsManifest, keyFetcher)
}

// WidevineJob holds configuration for a Widevine encrypted download.
type WidevineJob struct {
   Threads    int
   ClientId   string
   PrivateKey string
   Send       func([]byte) ([]byte, error)
}

// DownloadDash parses and downloads a Widevine-encrypted DASH stream.
func (j *WidevineJob) DownloadDash(body []byte, baseURL *url.URL, streamId string) error {
   keyFetcher := func(keyId, contentId []byte) ([]byte, error) {
      return j.widevineKey(keyId, contentId)
   }
   return runDownload(body, baseURL, j.Threads, streamId, dashManifest, keyFetcher)
}

// DownloadHls parses and downloads a Widevine-encrypted HLS stream.
func (j *WidevineJob) DownloadHls(body []byte, baseURL *url.URL, streamId string) error {
   keyFetcher := func(keyId, contentId []byte) ([]byte, error) {
      return j.widevineKey(keyId, contentId)
   }
   return runDownload(body, baseURL, j.Threads, streamId, hlsManifest, keyFetcher)
}

// --- Internal Helpers ---
// parseDash is an internal helper to parse a DASH manifest.
func parseDash(body []byte, baseURL *url.URL) (*dash.Mpd, error) {
   manifest, err := dash.Parse(body)
   if err != nil {
      return nil, fmt.Errorf("failed to parse DASH manifest: %w", err)
   }
   manifest.MpdUrl = baseURL
   return manifest, nil
}

// parseHls is an internal helper to parse an HLS master playlist.
func parseHls(body []byte, baseURL *url.URL) (*hls.MasterPlaylist, error) {
   bodyStr := string(body)
   master, err := hls.DecodeMaster(bodyStr)
   if err != nil {
      return nil, fmt.Errorf("failed to parse HLS playlist: %w", err)
   }
   master.ResolveURIs(baseURL)
   return master, nil
}

// listStreamsDash is an internal helper to print streams from a parsed manifest.
func listStreamsDash(manifest *dash.Mpd) error {
   sidxCache := make(map[string][]byte)
   groups := manifest.GetRepresentations()
   repsForSorting := make([]*dash.Representation, 0, len(groups))
   for _, group := range groups {
      representation := group[len(group)/2]
      if representation.GetMimeType() == "video/mp4" {
         if err := getMiddleBitrate(representation, sidxCache); err != nil {
            return fmt.Errorf("could not calculate bitrate for stream %s: %w", representation.Id, err)
         }
      }
      repsForSorting = append(repsForSorting, representation)
   }
   dash.SortByBandwidth(repsForSorting)
   for _, representation := range repsForSorting {
      fmt.Println(representation)
      fmt.Println()
   }
   return nil
}

// listStreamsHls is an internal helper to print streams from a parsed playlist.
func listStreamsHls(playlist *hls.MasterPlaylist) error {
   playlist.Sort()
   for _, rendition := range playlist.Medias {
      fmt.Println(rendition)
      fmt.Println()
   }
   for _, variant := range playlist.StreamInfs {
      fmt.Println(variant)
      fmt.Println()
   }
   return nil
}

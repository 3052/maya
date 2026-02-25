package maya

import (
   "41.neocities.org/luna/dash"
   "41.neocities.org/luna/hls"
   "encoding/xml"
   "flag"
   "fmt"
   "net/url"
   "os"
   "slices"
)

func Read[T any](name string) (*T, error) {
   data, err := os.ReadFile(name)
   if err != nil {
      return nil, err
   }
   var value T
   err = xml.Unmarshal(data, &value)
   if err != nil {
      return nil, err
   }
   return &value, nil
}

func Write(name string, value any) error {
   data, err := xml.Marshal(value)
   if err != nil {
      return err
   }
   // CHANGED: Use shared createFile to handle directories
   file, err := createFile(name)
   if err != nil {
      return err
   }
   defer file.Close()
   _, err = file.Write(data)
   return err
}

// listStreamsHls is an internal helper to print streams from a parsed playlist
func listStreamsHls(playlist *hls.MasterPlaylist) error {
   slices.SortFunc(playlist.Medias, hls.GroupId)
   slices.SortFunc(playlist.StreamInfs, hls.Bandwidth)

   var firstItemPrinted bool
   for _, rendition := range playlist.Medias {
      if firstItemPrinted {
         fmt.Println()
      } else {
         firstItemPrinted = true
      }
      fmt.Println(rendition)
   }
   for _, variant := range playlist.StreamInfs {
      if firstItemPrinted {
         fmt.Println()
      } else {
         firstItemPrinted = true
      }
      fmt.Println(variant)
   }
   return nil
}

// listStreamsDash is an internal helper to print streams from a parsed manifest
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
   slices.SortFunc(repsForSorting, dash.Bandwidth)
   for i, representation := range repsForSorting {
      if i > 0 {
         fmt.Println()
      }
      fmt.Println(representation)
   }
   return nil
}

// DownloadHls parses and downloads a clear HLS stream.
func (j *Job) DownloadHls(body []byte, baseURL *url.URL, streamId int) error {
   playlist, err := parseHls(body, baseURL)
   if err != nil {
      return err
   }
   return downloadHls(playlist, j.Threads, streamId, nil)
}

// Usage prints a usage message documenting all defined command-line flags.
// It returns an error if any of the specified flag names are not found.
func Usage(groups [][]string) error {
   for i, group := range groups {
      if i >= 1 {
         fmt.Println()
      }
      for _, name := range group {
         look := flag.Lookup(name)
         if look == nil {
            return fmt.Errorf("flag provided but not defined: -%s", name)
         }
         fmt.Printf("-%v %v\n", look.Name, look.Usage)
         if look.DefValue != "" {
            fmt.Printf("\tdefault %v\n", look.DefValue)
         }
      }
   }
   return nil
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
   manifest, err := parseDash(body, baseURL)
   if err != nil {
      return err
   }
   return downloadDash(manifest, j.Threads, streamId, nil)
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
   manifest, err := parseDash(body, baseURL)
   if err != nil {
      return err
   }
   return downloadDash(manifest, j.Threads, streamId, keyFetcher)
}

// DownloadHls parses and downloads a PlayReady-encrypted HLS stream.
func (j *PlayReadyJob) DownloadHls(body []byte, baseURL *url.URL, streamId int) error {
   keyFetcher := func(keyId, contentId []byte) ([]byte, error) {
      return j.playReadyKey(keyId)
   }
   playlist, err := parseHls(body, baseURL)
   if err != nil {
      return err
   }
   return downloadHls(playlist, j.Threads, streamId, keyFetcher)
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
   manifest, err := parseDash(body, baseURL)
   if err != nil {
      return err
   }
   return downloadDash(manifest, j.Threads, streamId, keyFetcher)
}

// DownloadHls parses and downloads a Widevine-encrypted HLS stream.
func (j *WidevineJob) DownloadHls(body []byte, baseURL *url.URL, streamId int) error {
   keyFetcher := func(keyId, contentId []byte) ([]byte, error) {
      return j.widevineKey(keyId, contentId)
   }
   playlist, err := parseHls(body, baseURL)
   if err != nil {
      return err
   }
   return downloadHls(playlist, j.Threads, streamId, keyFetcher)
}

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
   master.ResolveUris(baseURL)
   return master, nil
}

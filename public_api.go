package maya

import (
   "41.neocities.org/luna/dash"
   "41.neocities.org/luna/hls"
   "flag"
   "fmt"
   "net/url"
   "slices"
)

func Parse() map[string]bool {
   flag.Parse()
   set := map[string]bool{}
   flag.Visit(func(f *flag.Flag) {
      set[f.Name] = true
   })
   return set
}

func Usage(groups [][]string) error {
   seen := map[string]bool{}
   // 1. Print usage and mark flags as seen
   for i, group := range groups {
      if i >= 1 {
         fmt.Println()
      }
      for _, name := range group {
         look := flag.Lookup(name)
         if look == nil {
            return fmt.Errorf("flag provided but not defined: -%s", name)
         }
         seen[look.Name] = true
         fmt.Printf("-%v %v\n", look.Name, look.Usage)
         if look.DefValue != "" {
            fmt.Printf("\tdefault %v\n", look.DefValue)
         }
      }
   }
   // 2. Check for missing flags
   var missing string
   flag.VisitAll(func(f *flag.Flag) {
      if !seen[f.Name] {
         missing = f.Name
      }
   })
   if missing != "" {
      return fmt.Errorf("defined flag missing in groups: -%s", missing)
   }
   return nil
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
   groups := manifest.GetRepresentations()
   repsForSorting := make([]*dash.Representation, 0, len(groups))
   for _, group := range groups {
      representation := group[len(group)/2]
      if representation.GetMimeType() == "video/mp4" {
         if err := getMiddleBitrate(representation); err != nil {
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

// Sender encapsulates the process of sending a byte payload (like a signed
// license request) to a DRM server and returning the response payload.
type Sender func([]byte) ([]byte, error)

// Job holds configuration for downloads.
// It supports Clear, Widevine, and PlayReady via nested structs.
type Job struct {
   Threads   int
   Widevine  *WidevineJob
   PlayReady *PlayReadyJob
}

// WidevineJob holds credential paths for Widevine decryption.
type WidevineJob struct {
   ClientId   string
   PrivateKey string
}

// PlayReadyJob holds credential paths for PlayReady decryption.
type PlayReadyJob struct {
   CertificateChain string
   EncryptSignKey   string
}

// DownloadDash parses and downloads a DASH stream (Clear or Encrypted).
func (j *Job) DownloadDash(body []byte, baseURL *url.URL, streamId string, send Sender) error {
   manifest, err := parseDash(body, baseURL)
   if err != nil {
      return err
   }

   fetcher, err := j.getFetcher(send)
   if err != nil {
      return err
   }

   return downloadDash(manifest, j.Threads, streamId, fetcher)
}

// DownloadHls parses and downloads an HLS stream (Clear or Encrypted).
func (j *Job) DownloadHls(body []byte, baseURL *url.URL, streamId int, send Sender) error {
   playlist, err := parseHls(body, baseURL)
   if err != nil {
      return err
   }

   fetcher, err := j.getFetcher(send)
   if err != nil {
      return err
   }

   return downloadHls(playlist, j.Threads, streamId, fetcher)
}

// getFetcher determines the appropriate key retrieval logic based on which nested job struct is present.
func (j *Job) getFetcher(send Sender) (keyFetcher, error) {
   if j.Widevine != nil {
      if send == nil {
         return nil, fmt.Errorf("widevine configuration present but send function is nil")
      }
      return func(keyId, contentId []byte) ([]byte, error) {
         return j.Widevine.widevineKey(keyId, contentId, send)
      }, nil
   }
   if j.PlayReady != nil {
      if send == nil {
         return nil, fmt.Errorf("playready configuration present but send function is nil")
      }
      return func(keyId, contentId []byte) ([]byte, error) {
         return j.PlayReady.playReadyKey(keyId, send)
      }, nil
   }
   // Verify that we don't have a sender without a configuration
   if send != nil {
      return nil, fmt.Errorf("send function provided but no DRM configuration found")
   }
   // No DRM config present; return nil fetcher for clear download.
   return nil, nil
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

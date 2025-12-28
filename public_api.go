package maya

import (
   "41.neocities.org/luna/dash"
   "41.neocities.org/luna/hls"
   "fmt"
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

// --- Job Structs and Download Methods ---

// Job handles the core, non-DRM parameters for a download.
type Job struct {
   DASH    *dash.Mpd
   HLS     *hls.MasterPlaylist
   Threads int
}

// Download executes an unencrypted (clear) stream download.
func (j *Job) Download(streamId string) error {
   return runDownload(j.DASH, j.HLS, j.Threads, streamId, nil)
}

// PlayReadyJob handles PlayReady encrypted downloads.
type PlayReadyJob struct {
   Job              Job
   CertificateChain string
   EncryptSignKey   string
   Send             func([]byte) ([]byte, error)
}

// Download executes the download for a PlayReady encrypted stream.
func (j *PlayReadyJob) Download(streamId string) error {
   keyFetcher := func(keyID, contentID []byte) ([]byte, error) {
      return j.playReadyKey(keyID)
   }
   return runDownload(j.Job.DASH, j.Job.HLS, j.Job.Threads, streamId, keyFetcher)
}

// WidevineJob handles Widevine encrypted downloads.
type WidevineJob struct {
   Job        Job
   ClientID   string
   PrivateKey string
   Send       func([]byte) ([]byte, error)
}

// Download executes the download for a Widevine encrypted stream.
func (j *WidevineJob) Download(streamId string) error {
   keyFetcher := func(keyID, contentID []byte) ([]byte, error) {
      return j.widevineKey(keyID, contentID)
   }
   return runDownload(j.Job.DASH, j.Job.HLS, j.Job.Threads, streamId, keyFetcher)
}

// --- List Functions ---

func ListStreamsDash(manifest *dash.Mpd) error {
   sidxCache := make(map[string][]byte)
   groups := manifest.GetRepresentations()
   // 1. Collect a representative from each group and calculate missing bitrates.
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
   // 3. Print the sorted list.
   for _, representation := range repsForSorting {
      fmt.Println(representation)
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

package maya

import (
   "41.neocities.org/luna/dash"
   "41.neocities.org/luna/hls"
   "fmt"
   "net/http"
   "net/url"
   "slices"
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

// DownloadDASH retrieves a group of representations from a DASH manifest.
func (c *Config) DownloadDASH(manifest *dash.Mpd, key string) error {
   dashGroups := manifest.GetRepresentations()
   dashGroup, ok := dashGroups[key]
   if !ok {
      return fmt.Errorf("representation group not found %v", key)
   }

   // "Fetch-First" approach: Pre-fetch any necessary sidx data before starting the engine.
   sidxData := make(map[string][]byte)
   for _, rep := range dashGroup {
      if rep.SegmentBase != nil {
         baseUrl, err := rep.ResolveBaseUrl()
         if err != nil {
            return err
         }
         cacheKey := baseUrl.String() + rep.SegmentBase.IndexRange
         if _, exists := sidxData[cacheKey]; !exists {
            header := http.Header{}
            header.Set("Range", "bytes="+rep.SegmentBase.IndexRange)
            data, err := getSegment(baseUrl, header)
            if err != nil {
               return err
            }
            sidxData[cacheKey] = data
         }
      }
   }

   var group streamGroup
   for _, rep := range dashGroup {
      group = append(group, &dashStream{rep: rep, preFetchedSidx: sidxData})
   }
   return c.downloadGroupInternal(group)
}

// DownloadHLS retrieves a variant stream from an HLS playlist.
func (c *Config) DownloadHLS(playlist *hls.MasterPlaylist, key string) error {
   // For HLS, we treat each variant as a downloadable "group"
   variantIndex := 0 // Simple key mapping for now, "0", "1", etc.
   fmt.Sscanf(key, "%d", &variantIndex)
   if variantIndex >= len(playlist.Variants) {
      return fmt.Errorf("variant index not found %v", key)
   }
   variant := playlist.Variants[variantIndex]
   group := streamGroup{
      &hlsStream{
         variant: variant,
         baseURL: playlist.Variants[0].URI, // A bit of a simplification
         id:      fmt.Sprintf("hls-%d", variantIndex),
      },
   }
   return c.downloadGroupInternal(group)
}

// ListStreamsDASH parses and prints available streams from a DASH manifest.
func ListStreamsDASH(manifest *dash.Mpd) error {
   var middleStreams []stream
   for _, group := range manifest.GetRepresentations() {
      rep := group[len(group)/2]
      if rep.GetMimeType() == "video/mp4" {
         if err := getMiddleBitrate(rep); err != nil {
            return err
         }
      }
      middleStreams = append(middleStreams, &dashStream{rep: rep})
   }
   printStreams(middleStreams)
   return nil
}

// ListStreamsHLS parses and prints available streams from an HLS playlist.
func ListStreamsHLS(playlist *hls.MasterPlaylist) error {
   var streams []stream
   for i, variant := range playlist.Variants {
      streams = append(streams, &hlsStream{
         variant: variant,
         id:      fmt.Sprintf("%d", i),
      })
   }
   printStreams(streams)
   return nil
}

// printStreams is a shared helper for displaying stream info.
func printStreams(streams []stream) {
   slices.SortFunc(streams, func(a, b stream) int {
      return a.getBandwidth() - b.getBandwidth()
   })

   for index, s := range streams {
      if index >= 1 {
         fmt.Println()
      }
      fmt.Printf("ID: %s\nBandwidth: %d\nMimeType: %s\n",
         s.getID(), s.getBandwidth(), s.getMimeType())
   }
}

// Config holds downloader configuration
type Config struct {
   Send             func([]byte) ([]byte, error)
   Threads          int
   CertificateChain string
   EncryptSignKey   string
   ClientId         string
   PrivateKey       string
}

package maya

import (
   "41.neocities.org/luna/dash"
   "41.neocities.org/luna/hls"
   "fmt"
   "log"
   "net/http"
   "net/url"
   "slices"
   "strconv"
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
// The stream to download is specified by the StreamId field in the Config.
func (c *Config) DownloadDASH(manifest *dash.Mpd) error {
   dashGroups := manifest.GetRepresentations()
   dashGroup, ok := dashGroups[c.StreamId]
   if !ok {
      return fmt.Errorf("representation group not found %v", c.StreamId)
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

// DownloadHLS retrieves a variant or rendition stream from an HLS playlist by its ID.
func (c *Config) DownloadHLS(playlist *hls.MasterPlaylist) error {
   keyInt, err := strconv.Atoi(c.StreamId)
   if err != nil {
      return fmt.Errorf("invalid HLS variant StreamId, must be an integer: %q", c.StreamId)
   }

   // Find the target stream, which can be either a Variant or a Rendition.
   var targetStream stream
   baseURL := playlist.Variants[0].URI // Assume base URL can be derived from the first variant.

   for _, v := range playlist.Variants {
      if v.ID == keyInt {
         targetStream = &hlsVariantStream{variant: v, baseURL: baseURL}
         break
      }
   }
   if targetStream == nil {
      for _, r := range playlist.Medias {
         if r.ID == keyInt {
            targetStream = &hlsRenditionStream{rendition: r, baseURL: baseURL}
            break
         }
      }
   }

   if targetStream == nil {
      return fmt.Errorf("stream with ID not found: %d", keyInt)
   }

   group := streamGroup{targetStream}
   return c.downloadGroupInternal(group)
}

// ListStreamsDASH parses and prints available streams from a DASH manifest.
func ListStreamsDASH(manifest *dash.Mpd) error {
   var middleStreams []stream
   for _, group := range manifest.GetRepresentations() {
      rep := group[len(group)/2]
      // Restore the check to only calculate bitrate for video streams.
      if rep.GetMimeType() == "video/mp4" {
         if err := getMiddleBitrate(rep); err != nil {
            log.Printf("Could not calculate bitrate for stream %s: %v", rep.Id, err)
         }
      }
      middleStreams = append(middleStreams, &dashStream{rep: rep})
   }
   printStreams(middleStreams)
   return nil
}

// ListStreamsHLS parses and prints all available streams (variants and renditions) from an HLS playlist.
func ListStreamsHLS(playlist *hls.MasterPlaylist) error {
   var streams []stream
   baseURL := playlist.Variants[0].URI // Base for resolving relative rendition URIs.

   for _, variant := range playlist.Variants {
      streams = append(streams, &hlsVariantStream{
         variant: variant,
         baseURL: baseURL,
      })
   }
   for _, rendition := range playlist.Medias {
      streams = append(streams, &hlsRenditionStream{
         rendition: rendition,
         baseURL:   baseURL,
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
      // fmt.Println will automatically call the String() method on the stream.
      fmt.Println(s)
   }
}

// Config holds downloader configuration.
type Config struct {
   Send             func([]byte) ([]byte, error)
   Threads          int
   CertificateChain string
   EncryptSignKey   string
   ClientId         string
   PrivateKey       string
   // StreamId is the identifier of the stream to download (e.g., "0", "1", etc.).
   StreamId string
}

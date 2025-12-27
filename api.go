package maya

import (
   "41.neocities.org/luna/dash"
   "41.neocities.org/luna/hls"
   "41.neocities.org/sofia"
   "fmt"
   "log"
   "net/http"
   "net/url"
   "os"
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
func (c *Config) DownloadDASH(manifest *dash.Mpd) error {
   dashGroup, ok := manifest.GetRepresentations()[c.StreamId]
   if !ok {
      return fmt.Errorf("representation group not found %v", c.StreamId)
   }
   if len(dashGroup) == 0 {
      return fmt.Errorf("representation group is empty")
   }
   rep := dashGroup[0] // Use the first representation for metadata.

   // Step 1: Pre-fetch sidx data for the entire group.
   var sidxData []byte
   if rep.SegmentBase != nil {
      baseUrl, err := rep.ResolveBaseUrl()
      if err != nil {
         return err
      }
      header := http.Header{}
      header.Set("Range", "bytes="+rep.SegmentBase.IndexRange)
      sidxData, err = getSegment(baseUrl, header)
      if err != nil {
         return err
      }
   }

   // Step 2: Download the first segment for content detection.
   var firstData []byte
   var err error
   if rep.SegmentBase != nil && rep.SegmentBase.Initialization != nil {
      baseUrl, _ := rep.ResolveBaseUrl()
      header := http.Header{"Range": []string{"bytes=" + rep.SegmentBase.Initialization.Range}}
      firstData, err = getSegment(baseUrl, header)
   } else {
      // Use the full segment generator to get the first media segment.
      segs, err_segs := generateSegments(rep)
      if err_segs != nil {
         return err_segs
      }
      if len(segs) == 0 {
         return nil
      }
      firstData, err = getSegment(segs[0].url, segs[0].header)
   }
   if err != nil {
      return fmt.Errorf("failed to download first segment for content detection: %w", err)
   }

   // Step 3: Detect, create file, and configure DRM.
   detection := detectContentType(firstData)
   if detection.Extension == "" {
      return fmt.Errorf("could not determine file type for stream %s", rep.Id)
   }
   fileName := rep.Id + detection.Extension
   log.Println("Create", fileName)
   file, err := os.Create(fileName)
   if err != nil {
      return err
   }
   defer file.Close()

   var media mediaFile
   if c.isDrmNeeded() {
      protection, _ := getDashProtection(rep, c.activeDrmScheme())
      if protection != nil {
         if err := media.configureProtection(protection); err != nil {
            return err
         }
      }
   }

   // Step 4: Prepare remuxer and process the first segment.
   var remux *sofia.Remuxer
   if detection.IsFMP4 {
      remux, err = media.initializeWriter(file, firstData)
      if err != nil {
         return err
      }
   } else {
      if _, err := file.Write(firstData); err != nil {
         return err
      }
   }

   // Step 5: Get key and all media requests.
   key, err := c.fetchKey(&media)
   if err != nil {
      return err
   }

   requests, err := getDashMediaRequests(dashGroup, sidxData)
   if err != nil {
      return err
   }

   // Step 6: Execute the download for the remaining segments.
   remainingRequests := requests
   if len(requests) > 0 && !(rep.SegmentBase != nil && rep.SegmentBase.Initialization != nil) {
      remainingRequests = requests[1:]
   }

   return c.executeDownload(remainingRequests, key, remux, file)
}

// DownloadHLS retrieves a variant or rendition stream from an HLS playlist.
func (c *Config) DownloadHLS(playlist *hls.MasterPlaylist) error {
   keyInt, err := strconv.Atoi(c.StreamId)
   if err != nil {
      return fmt.Errorf("invalid HLS variant StreamId, must be an integer: %q", c.StreamId)
   }
   baseURL := playlist.Variants[0].URI

   // Find the target, which can be a Variant or Rendition.
   var targetURI *url.URL
   var targetID string
   var protection *protectionInfo
   var segments []segment

   for _, v := range playlist.Variants {
      if v.ID == keyInt {
         targetURI = v.URI
         targetID = strconv.Itoa(v.ID)
         mediaPl, err_pl := fetchMediaPlaylist(targetURI, baseURL)
         if err_pl != nil {
            return err_pl
         }
         protection, _ = getHlsProtection(mediaPl, c.activeDrmScheme())
         segments, err = hlsSegments(mediaPl)
         if err != nil {
            return err
         }
         break
      }
   }
   if targetURI == nil {
      for _, r := range playlist.Medias {
         if r.ID == keyInt {
            targetURI = r.URI
            targetID = strconv.Itoa(r.ID)
            mediaPl, err_pl := fetchMediaPlaylist(targetURI, baseURL)
            if err_pl != nil {
               return err_pl
            }
            protection, _ = getHlsProtection(mediaPl, c.activeDrmScheme())
            segments, err = hlsSegments(mediaPl)
            if err != nil {
               return err
            }
            break
         }
      }
   }
   if targetURI == nil {
      return fmt.Errorf("stream with ID not found: %d", keyInt)
   }

   // Step 2 & 3 (combined): Download first segment, detect, create file.
   if len(segments) == 0 {
      return nil
   }
   firstData, err := getSegment(segments[0].url, segments[0].header)
   if err != nil {
      return err
   }

   detection := detectContentType(firstData)
   if detection.Extension == "" {
      return fmt.Errorf("could not determine file type for stream %s", targetID)
   }
   fileName := targetID + detection.Extension
   log.Println("Create", fileName)
   file, err := os.Create(fileName)
   if err != nil {
      return err
   }
   defer file.Close()

   var media mediaFile
   if protection != nil {
      if err := media.configureProtection(protection); err != nil {
         return err
      }
   }

   // Step 4: Prepare remuxer.
   var remux *sofia.Remuxer
   if detection.IsFMP4 {
      remux, err = media.initializeWriter(file, firstData)
      if err != nil {
         return err
      }
   } else {
      if _, err := file.Write(firstData); err != nil {
         return err
      }
   }

   // Step 5: Get key and requests.
   key, err := c.fetchKey(&media)
   if err != nil {
      return err
   }

   requests := make([]mediaRequest, len(segments))
   for i, s := range segments {
      requests[i] = mediaRequest{url: s.url, header: s.header}
   }

   // Step 6: Execute download.
   return c.executeDownload(requests[1:], key, remux, file)
}

// ListStreamsDASH parses and prints available streams from a DASH manifest.
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

// ListStreamsHLS parses and prints all available streams (variants and renditions) from an HLS playlist.
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

func (c *Config) isDrmNeeded() bool {
   return (c.CertificateChain != "" && c.EncryptSignKey != "") || (c.ClientId != "" && c.PrivateKey != "")
}

func (c *Config) activeDrmScheme() string {
   if c.CertificateChain != "" && c.EncryptSignKey != "" {
      return "playready"
   }
   if c.ClientId != "" && c.PrivateKey != "" {
      return "widevine"
   }
   return ""
}

// hls.go
package maya

import (
   "41.neocities.org/luna/hls"
   "errors"
   "fmt"
   "io"
   "net/http"
   "net/url"
   "slices"
   "strconv"
   "strings"
)

// fetchMediaPlaylist fetches and parses an HLS media playlist.
func fetchMediaPlaylist(mediaURL *url.URL) (*hls.MediaPlaylist, error) {
   resp, err := http.Get(mediaURL.String())
   if err != nil {
      return nil, err
   }
   defer resp.Body.Close()
   if resp.StatusCode != http.StatusOK {
      return nil, errors.New(resp.Status)
   }
   var data strings.Builder
   _, err = io.Copy(&data, resp.Body)
   if err != nil {
      return nil, err
   }
   mediaPl, err := hls.DecodeMedia(data.String())
   if err != nil {
      return nil, err
   }
   mediaPl.ResolveUris(mediaURL)
   return mediaPl, nil
}

// downloadHls parses an HLS manifest, extracts all necessary data, and passes it to the central orchestrator.
func downloadHls(playlist *hls.MasterPlaylist, threads int, streamId int, fetchKey keyFetcher) error {
   typeInfo, targetURI, err := detectHlsType(playlist, streamId)
   if err != nil {
      return err
   }
   mediaPl, err := fetchMediaPlaylist(targetURI)
   if err != nil {
      return err
   }

   allRequests := make([]segment, len(mediaPl.Segments))
   for i, hlsSeg := range mediaPl.Segments {
      allRequests[i] = segment{url: hlsSeg.Uri, header: nil}
   }

   var initData []byte
   if typeInfo.IsFMP4 && mediaPl.Map != nil {
      initData, err = getSegment(mediaPl.Map, nil)
      if err != nil {
         return fmt.Errorf("failed to get HLS initialization segment: %w", err)
      }
   }
   job := &downloadJob{
      outputFileNameBase: strconv.Itoa(streamId),
      typeInfo:           typeInfo,
      allRequests:        allRequests,
      initSegmentData:    initData,
      manifestProtection: nil, // No manifest protection extraction for HLS
      threads:            threads,
      fetchKey:           fetchKey,
   }
   return orchestrateDownload(job)
}

// detectHlsType finds the correct stream in an HLS playlist by its ID and determines its type.
func detectHlsType(playlist *hls.MasterPlaylist, streamId int) (*typeInfo, *url.URL, error) {
   for _, variant := range playlist.StreamInfs {
      if variant.Id == streamId {
         info := &typeInfo{Extension: ".mp4", IsFMP4: true}
         return info, variant.Uri, nil
      }
   }
   for _, rendition := range playlist.Medias {
      if rendition.Id == streamId {
         var info *typeInfo
         switch rendition.Type {
         case "AUDIO":
            info = &typeInfo{Extension: ".m4a", IsFMP4: true}
         case "SUBTITLES":
            info = &typeInfo{Extension: ".vtt", IsFMP4: false}
         default:
            return nil, nil, fmt.Errorf("unsupported HLS media type: %s", rendition.Type)
         }
         return info, rendition.Uri, nil
      }
   }
   return nil, nil, fmt.Errorf("stream with ID not found: %d", streamId)
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

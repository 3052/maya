// hls.go
package maya

import (
   "41.neocities.org/luna/hls"
   "errors"
   "fmt"
   "io"
   "net/http"
   "net/url"
   "path"
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
   targetURI, err := getHlsStreamUrl(playlist, streamId)
   if err != nil {
      return err
   }
   mediaPl, err := fetchMediaPlaylist(targetURI)
   if err != nil {
      return err
   }

   typeInfo, err := determineHlsType(mediaPl)
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

// getHlsStreamUrl finds the correct stream in an HLS playlist by its ID and returns its URI.
func getHlsStreamUrl(playlist *hls.MasterPlaylist, streamId int) (*url.URL, error) {
   for _, variant := range playlist.StreamInfs {
      if variant.Id == streamId {
         return variant.Uri, nil
      }
   }
   for _, rendition := range playlist.Medias {
      if rendition.Id == streamId {
         return rendition.Uri, nil
      }
   }
   return nil, fmt.Errorf("stream with ID not found: %d", streamId)
}

// determineHlsType extracts the file extension directly from the segment URL.
func determineHlsType(mediaPl *hls.MediaPlaylist) (*typeInfo, error) {
   if len(mediaPl.Segments) == 0 {
      return nil, errors.New("empty media playlist")
   }

   // Rely entirely on the URL of the first segment
   firstSegURL := mediaPl.Segments[0].Uri
   ext := path.Ext(firstSegURL.Path)
   if ext == "" {
      return nil, fmt.Errorf("no file extension found in segment URL: %s", firstSegURL.String())
   }

   if ext == ".mp4a" {
      ext = ".m4a"
   }

   // If it has a Map, it's definitively fMP4. Otherwise, check if the URL explicitly says it's an mp4 variant.
   isFMP4 := false
   if mediaPl.Map != nil {
      isFMP4 = true
   } else if ext == ".mp4" || ext == ".m4s" || ext == ".m4a" {
      isFMP4 = true
   }

   return &typeInfo{
      Extension: ext,
      IsFMP4:    isFMP4,
   }, nil
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

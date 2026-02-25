package maya

import (
   "41.neocities.org/luna/hls"
   "fmt"
   "io"
   "net/http"
   "net/url"
   "strconv"
)

// fetchMediaPlaylist fetches and parses an HLS media playlist.
func fetchMediaPlaylist(mediaURL *url.URL) (*hls.MediaPlaylist, error) {
   if mediaURL == nil {
      return nil, fmt.Errorf("HLS stream has no URI")
   }
   resp, err := http.Get(mediaURL.String())
   if err != nil {
      return nil, err
   }
   defer resp.Body.Close()
   body, err := io.ReadAll(resp.Body)
   if err != nil {
      return nil, err
   }
   mediaPl, err := hls.DecodeMedia(string(body))
   if err != nil {
      return nil, err
   }
   mediaPl.ResolveUris(mediaURL)
   return mediaPl, nil
}

// hlsSegments generates a list of segments from a media playlist.
func hlsSegments(mediaPl *hls.MediaPlaylist) ([]segment, error) {
   var segments []segment
   for _, hlsSeg := range mediaPl.Segments {
      segments = append(segments, segment{url: hlsSeg.Uri, header: nil})
   }
   return segments, nil
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
   hlsSegs, err := hlsSegments(mediaPl)
   if err != nil {
      return err
   }
   allRequests := make([]mediaRequest, len(hlsSegs))
   for i, seg := range hlsSegs {
      allRequests[i] = mediaRequest{url: seg.url, header: seg.header}
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

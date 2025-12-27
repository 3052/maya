package maya

import (
   "41.neocities.org/luna/dash"
   "41.neocities.org/luna/hls"
   "fmt"
   "io"
   "net/http"
   "net/url"
   "strings"
)

// streamGroup represents a collection of related streams.
type streamGroup []stream

// stream represents a single media stream (e.g., a specific resolution/bitrate).
// It returns a slice of the 'segment' struct which is defined in dash_segments.go
type stream interface {
   getMimeType() string
   getSegments() ([]segment, error)
   getInitSegment() (*segment, error)
   getProtection() ([]protectionInfo, error)
   getID() string
   getBandwidth() int
}

// protectionInfo holds standardized DRM information.
type protectionInfo struct {
   Scheme string
   Pssh   []byte
   KeyID  []byte
}

// --- DASH Stream Implementation ---

type dashStream struct {
   rep            *dash.Representation
   preFetchedSidx map[string][]byte // Contains sidx data, fetched by the main DownloadDASH function.
}

// getSegments now uses the pre-fetched sidx data passed during its creation.
func (s *dashStream) getSegments() ([]segment, error) {
   if s.rep.SegmentBase != nil {
      baseUrl, err := s.rep.ResolveBaseUrl()
      if err != nil {
         return nil, err
      }
      cacheKey := baseUrl.String() + s.rep.SegmentBase.IndexRange

      sidxData, found := s.preFetchedSidx[cacheKey]
      if !found {
         // This should not happen in the new architecture, but we can handle it gracefully.
         return nil, fmt.Errorf("sidx data for key %s not found in pre-fetched map", cacheKey)
      }
      return generateSegmentsFromSidx(s.rep, sidxData)
   }

   // For other types (SegmentTemplate, SegmentList), the logic is unchanged.
   return generateSegments(s.rep)
}

func (s *dashStream) getInitSegment() (*segment, error) {
   var targetUrl *url.URL
   var header http.Header
   var err error
   if s.rep.SegmentBase != nil && s.rep.SegmentBase.Initialization != nil {
      header = make(http.Header)
      header.Set("Range", "bytes="+s.rep.SegmentBase.Initialization.Range)
      targetUrl, err = s.rep.ResolveBaseUrl()
   } else if tmpl := s.rep.GetSegmentTemplate(); tmpl != nil && tmpl.Initialization != "" {
      targetUrl, err = tmpl.ResolveInitialization(s.rep)
   } else if s.rep.SegmentList != nil && s.rep.SegmentList.Initialization != nil {
      targetUrl, err = s.rep.SegmentList.Initialization.ResolveSourceUrl()
   }
   if err != nil || targetUrl == nil {
      return nil, err
   }
   return &segment{url: targetUrl, header: header}, nil
}

func (s *dashStream) getProtection() ([]protectionInfo, error) {
   const widevineURN = "urn:uuid:edef8ba9-79d6-4ace-a3c8-27dcd51d21ed"
   var protections []protectionInfo
   for _, cp := range s.rep.GetContentProtection() {
      var info protectionInfo
      if strings.ToLower(cp.SchemeIdUri) == widevineURN {
         info.Scheme = "widevine"
      } else {
         continue
      }
      pssh, _ := cp.GetPssh()
      info.Pssh = pssh
      kid, _ := cp.GetDefaultKid()
      info.KeyID = kid
      protections = append(protections, info)
   }
   return protections, nil
}

func (s *dashStream) getMimeType() string { return s.rep.GetMimeType() }
func (s *dashStream) getID() string       { return s.rep.Id }
func (s *dashStream) getBandwidth() int   { return s.rep.Bandwidth }

// --- HLS Stream Implementation ---

type hlsStream struct {
   variant       *hls.Variant
   baseURL       *url.URL
   id            string
   mediaPlaylist *hls.MediaPlaylist // Cache
}

func (s *hlsStream) fetchAndParseMediaPlaylist() (*hls.MediaPlaylist, error) {
   if s.mediaPlaylist != nil {
      return s.mediaPlaylist, nil
   }
   if s.variant.URI == nil {
      return nil, fmt.Errorf("HLS variant has no URI")
   }
   mediaURL := s.baseURL.ResolveReference(s.variant.URI)
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
   mediaPl.ResolveURIs(mediaURL)
   s.mediaPlaylist = mediaPl
   return mediaPl, nil
}

func (s *hlsStream) getSegments() ([]segment, error) {
   mediaPl, err := s.fetchAndParseMediaPlaylist()
   if err != nil {
      return nil, err
   }
   var segments []segment
   for _, hlsSeg := range mediaPl.Segments {
      segments = append(segments, segment{url: hlsSeg.URI, header: nil})
   }
   return segments, nil
}

func (s *hlsStream) getInitSegment() (*segment, error) {
   mediaPl, err := s.fetchAndParseMediaPlaylist()
   if err != nil {
      return nil, err
   }
   if len(mediaPl.Segments) > 0 && mediaPl.Segments[0].Map != nil {
      return &segment{url: mediaPl.Segments[0].Map}, nil
   }
   return nil, nil
}

func (s *hlsStream) getProtection() ([]protectionInfo, error) {
   mediaPl, err := s.fetchAndParseMediaPlaylist()
   if err != nil {
      return nil, err
   }
   var protections []protectionInfo
   if len(mediaPl.Keys) > 0 {
      hlsKey := mediaPl.Keys[0]
      if strings.Contains(hlsKey.KeyFormat, "widevine") && hlsKey.URI != nil && hlsKey.URI.Scheme == "data" {
         psshData, err := hlsKey.DecodeData()
         if err == nil {
            protections = append(protections, protectionInfo{Scheme: "widevine", Pssh: psshData})
         }
      }
   }
   return protections, nil
}

func (s *hlsStream) getMimeType() string {
   if strings.Contains(s.variant.Codecs, "avc1") || strings.Contains(s.variant.Codecs, "hev1") {
      return "video/mp4"
   }
   if strings.Contains(s.variant.Codecs, "mp4a") {
      return "audio/mp4"
   }
   return "video/mp2t"
}

func (s *hlsStream) getID() string     { return s.id }
func (s *hlsStream) getBandwidth() int { return s.variant.Bandwidth }

package maya

import (
   "41.neocities.org/luna/dash"
   "41.neocities.org/luna/hls"
   "fmt"
   "io"
   "net/http"
   "net/url"
   "strconv"
   "strings"
)

// streamGroup represents a collection of related streams.
type streamGroup []stream

// stream represents a single media stream with a common set of methods.
type stream interface {
   getSegments() ([]segment, error)
   getInitSegment() (*segment, error)
   getProtection() ([]protectionInfo, error)
   getID() string
   getBandwidth() int
   String() string
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
   preFetchedSidx map[string][]byte
}

func (s *dashStream) getSegments() ([]segment, error) {
   if s.rep.SegmentBase != nil {
      baseUrl, err := s.rep.ResolveBaseUrl()
      if err != nil {
         return nil, err
      }
      cacheKey := baseUrl.String() + s.rep.SegmentBase.IndexRange
      sidxData, found := s.preFetchedSidx[cacheKey]
      if !found {
         return nil, fmt.Errorf("sidx data for key %s not found in pre-fetched map", cacheKey)
      }
      return generateSegmentsFromSidx(s.rep, sidxData)
   }
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

func (s *dashStream) getID() string     { return s.rep.Id }
func (s *dashStream) getBandwidth() int { return s.rep.Bandwidth }
func (s *dashStream) String() string    { return s.rep.String() }

// --- HLS Stream Implementations ---

// fetchMediaPlaylist is a shared helper for both HLS variant and rendition streams.
func fetchMediaPlaylist(uri, base *url.URL) (*hls.MediaPlaylist, error) {
   if uri == nil {
      return nil, fmt.Errorf("HLS stream has no URI")
   }
   mediaURL := base.ResolveReference(uri)
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
   return mediaPl, nil
}

// hlsVariantStream adapts an hls.Variant to the stream interface.
type hlsVariantStream struct {
   variant       *hls.Variant
   baseURL       *url.URL
   mediaPlaylist *hls.MediaPlaylist // Cache
}

func (s *hlsVariantStream) fetchPlaylist() (*hls.MediaPlaylist, error) {
   if s.mediaPlaylist == nil {
      pl, err := fetchMediaPlaylist(s.variant.URI, s.baseURL)
      if err != nil {
         return nil, err
      }
      s.mediaPlaylist = pl
   }
   return s.mediaPlaylist, nil
}

func (s *hlsVariantStream) getSegments() ([]segment, error) {
   mediaPl, err := s.fetchPlaylist()
   if err != nil {
      return nil, err
   }
   var segments []segment
   for _, hlsSeg := range mediaPl.Segments {
      segments = append(segments, segment{url: hlsSeg.URI, header: nil})
   }
   return segments, nil
}

func (s *hlsVariantStream) getInitSegment() (*segment, error) {
   mediaPl, err := s.fetchPlaylist()
   if err != nil {
      return nil, err
   }
   if len(mediaPl.Segments) > 0 && mediaPl.Segments[0].Map != nil {
      return &segment{url: mediaPl.Segments[0].Map}, nil
   }
   return nil, nil
}

func (s *hlsVariantStream) getProtection() ([]protectionInfo, error) {
   mediaPl, err := s.fetchPlaylist()
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

func (s *hlsVariantStream) getID() string     { return strconv.Itoa(s.variant.ID) }
func (s *hlsVariantStream) getBandwidth() int { return s.variant.Bandwidth }
func (s *hlsVariantStream) String() string    { return s.variant.String() }

// hlsRenditionStream adapts an hls.Rendition to the stream interface.
type hlsRenditionStream struct {
   rendition     *hls.Rendition
   baseURL       *url.URL
   mediaPlaylist *hls.MediaPlaylist // Cache
}

func (s *hlsRenditionStream) fetchPlaylist() (*hls.MediaPlaylist, error) {
   if s.mediaPlaylist == nil {
      pl, err := fetchMediaPlaylist(s.rendition.URI, s.baseURL)
      if err != nil {
         return nil, err
      }
      s.mediaPlaylist = pl
   }
   return s.mediaPlaylist, nil
}

func (s *hlsRenditionStream) getSegments() ([]segment, error) {
   mediaPl, err := s.fetchPlaylist()
   if err != nil {
      return nil, err
   }
   var segments []segment
   for _, hlsSeg := range mediaPl.Segments {
      segments = append(segments, segment{url: hlsSeg.URI, header: nil})
   }
   return segments, nil
}

func (s *hlsRenditionStream) getInitSegment() (*segment, error) {
   mediaPl, err := s.fetchPlaylist()
   if err != nil {
      return nil, err
   }
   if len(mediaPl.Segments) > 0 && mediaPl.Segments[0].Map != nil {
      return &segment{url: mediaPl.Segments[0].Map}, nil
   }
   return nil, nil
}

func (s *hlsRenditionStream) getProtection() ([]protectionInfo, error) {
   mediaPl, err := s.fetchPlaylist()
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

func (s *hlsRenditionStream) getID() string     { return strconv.Itoa(s.rendition.ID) }
func (s *hlsRenditionStream) getBandwidth() int { return 0 } // Renditions don't have bandwidth.
func (s *hlsRenditionStream) String() string    { return s.rendition.String() }

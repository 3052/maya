package net

import (
   "41.neocities.org/dash"
   "41.neocities.org/sofia"
   "bytes"
   "encoding/base64"
   "encoding/hex"
   "errors"
   "log"
   "net/http"
   "slices"
)

func (m *media_file) initialization(data []byte) ([]byte, error) {
   parsedInit, err := sofia.Parse(data)
   if err != nil {
      return nil, err
   }
   moov, ok := sofia.FindMoov(parsedInit)
   if !ok {
      return nil, errors.New("could not find 'moov' box in init file")
   }
   trak, ok := moov.Trak()
   if !ok {
      return nil, errors.New("could not find 'trak' in moov")
   }
   mdhd, ok := trak.Mdhd()
   if !ok {
      return nil, errors.New("could not find 'mdhd' in trak to get timescale")
   }
   m.timescale = uint64(mdhd.Timescale)
   err = moov.Sanitize()
   if err != nil {
      return nil, err
   }
   widevine_box, ok := moov.FindPssh(widevine_id)
   if ok {
      m.pssh = widevine_box.Data
   }
   m.key_id = trak.Tenc().DefaultKID[:]
   var finalMP4Data bytes.Buffer
   for _, box := range parsedInit {
      finalMP4Data.Write(box.Encode())
   }
   return finalMP4Data.Bytes(), nil
}

type media_file struct {
   key_id    []byte // tenc
   pssh      []byte // pssh
   timescale uint64 // mdhd
   size      uint64 // trun
   duration  uint64 // trun
}

func (m *media_file) New(represent *dash.Representation) error {
   for _, content := range represent.ContentProtection {
      if content.SchemeIdUri == widevine_urn {
         if content.Pssh != "" {
            data, err := base64.StdEncoding.DecodeString(content.Pssh)
            if err != nil {
               return err
            }
            var box sofia.PsshBox
            err = box.Parse(data)
            if err != nil {
               return err
            }
            m.pssh = box.Data
            break
         }
      }
   }
   return nil
}

var widevine_id, _ = hex.DecodeString("edef8ba979d64acea3c827dcd51d21ed")

func (c *Config) get_media_requests(represent *dash.Representation) ([]media_request, error) {
   switch {
   case represent.SegmentList != nil:
      requests := make([]media_request, len(represent.SegmentList.SegmentUrl))
      for i, segment := range represent.SegmentList.SegmentUrl {
         requests[i] = media_request{url: segment.Media[0]}
      }
      return requests, nil
   case represent.SegmentTemplate != nil:
      var segments []int
      for rep := range represent.Representation() {
         segments = slices.AppendSeq(segments, rep.Segment())
      }
      requests := make([]media_request, len(segments))
      for i, segment := range segments {
         address, err := represent.SegmentTemplate.Media.Url(represent, segment)
         if err != nil {
            return nil, err
         }
         requests[i] = media_request{url: address}
      }
      return requests, nil
   case represent.SegmentBase != nil:
      head := http.Header{}
      head.Set("range", "bytes="+represent.SegmentBase.IndexRange)
      data, err := get_segment(represent.BaseUrl[0], head)
      if err != nil {
         return nil, err
      }
      parsed, err := sofia.Parse(data)
      if err != nil {
         return nil, err
      }
      var index index_range
      if err = index.Set(represent.SegmentBase.IndexRange); err != nil {
         return nil, err
      }
      sidx, ok := sofia.FindSidx(parsed)
      if !ok {
         return nil, errors.New("sidx box not found in file")
      }
      requests := make([]media_request, len(sidx.References))
      for i, reference := range sidx.References {
         index[0] = index[1] + 1
         index[1] += uint64(reference.ReferencedSize)
         range_head := http.Header{}
         range_head.Set("range", "bytes="+index.String())
         requests[i] = media_request{
            url: represent.BaseUrl[0], header: range_head,
         }
      }
      return requests, nil
   }
   return nil, errors.New("unsupported segment type")
}

// segment can be VTT or anything
func (m *media_file) write_segment(data, key []byte) ([]byte, error) {
   if key == nil {
      return data, nil
   }
   parsedSegment, err := sofia.Parse(data)
   if err != nil {
      return nil, err
   }
   if m.duration/m.timescale < 10*60 {
      for _, moof := range sofia.AllMoof(parsedSegment) {
         traf, ok := moof.Traf()
         if !ok {
            return nil, errors.New("could not find 'traf' box in segment file")
         }
         total_bytes, total_duration, err := traf.Totals()
         if err != nil {
            return nil, err
         }
         m.size += total_bytes
         m.duration += total_duration
      }
      // Bandwidth in bps = (TotalBytes * 8 bits/byte) /
      // (TotalDuration / Timescale in seconds)
      // Simplified: (TotalBytes * 8 * Timescale) / TotalDuration
      log.Println("bandwidth", m.size * 8 * m.timescale / m.duration)
   }
   err = sofia.Decrypt(parsedSegment, key)
   if err != nil {
      return nil, err
   }
   var finalMP4Data bytes.Buffer
   for _, box := range parsedSegment {
      finalMP4Data.Write(box.Encode())
   }
   return finalMP4Data.Bytes(), nil
}

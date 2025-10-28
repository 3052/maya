package net

import (
   "41.neocities.org/dash"
   "41.neocities.org/sofia"
   "encoding/base64"
   "errors"
   "log"
   "net/http"
   "slices"
)

// segment can be VTT or anything
func (m *media_file) write_segment(data, key []byte) ([]byte, error) {
   if key == nil {
      return data, nil
   }
   var fileVar file.File
   err := fileVar.Read(data)
   if err != nil {
      return nil, err
   }
   if m.duration/m.timescale < 10*60 {
      for _, sample := range fileVar.Moof.Traf.Trun.Sample {
         if sample.Duration == 0 {
            sample.Duration = fileVar.Moof.Traf.Tfhd.DefaultSampleDuration
         }
         m.duration += uint64(sample.Duration)
         if sample.Size == 0 {
            sample.Size = fileVar.Moof.Traf.Tfhd.DefaultSampleSize
         }
         m.size += uint64(sample.Size)
      }
      log.Println("bandwidth", m.timescale*m.size*8/m.duration)
   }
   if fileVar.Moof.Traf.Senc == nil {
      return data, nil
   }
   for i, data := range fileVar.Mdat.Data(&fileVar.Moof.Traf) {
      err = fileVar.Moof.Traf.Senc.Sample[i].Decrypt(data, key)
      if err != nil {
         return nil, err
      }
   }
   return fileVar.Append(nil)
}

func (m *media_file) initialization(data []byte) ([]byte, error) {
   var fileVar file.File
   err := fileVar.Read(data)
   if err != nil {
      return nil, err
   }
   // Moov
   moov, ok := fileVar.GetMoov()
   if !ok {
      return data, nil
   }
   // Moov.Pssh
   for _, psshVar := range moov.Pssh {
      if psshVar.SystemId.String() == widevine_system_id {
         m.pssh = psshVar.Data
      }
      copy(psshVar.BoxHeader.Type[:], "free") // Firefox
   }
   // Moov.Trak
   m.timescale = uint64(moov.Trak.Mdia.Mdhd.Timescale)
   // Sinf
   sinf, ok := moov.Trak.Mdia.Minf.Stbl.Stsd.Sinf()
   if !ok {
      return data, nil
   }
   // Sinf.BoxHeader
   copy(sinf.BoxHeader.Type[:], "free") // Firefox
   // Sinf.Schi
   m.key_id = sinf.Schi.Tenc.DefaultKid[:]
   // SampleEntry
   sample, ok := moov.Trak.Mdia.Minf.Stbl.Stsd.SampleEntry()
   if !ok {
      return data, nil
   }
   // SampleEntry.BoxHeader
   sample.BoxHeader.Type = sinf.Frma.DataFormat // Firefox
   return fileVar.Append(nil)
}

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
      var file_file file.File
      if err = file_file.Read(data); err != nil {
         return nil, err
      }
      var index index_range
      if err = index.Set(represent.SegmentBase.IndexRange); err != nil {
         return nil, err
      }
      requests := make([]media_request, len(file_file.Sidx.Reference))
      for i, reference := range file_file.Sidx.Reference {
         index[0] = index[1] + 1
         index[1] += uint64(reference.Size())
         range_head := http.Header{}
         range_head.Set("range", "bytes="+index.String())
         requests[i] = media_request{url: represent.BaseUrl[0], header: range_head}
      }
      return requests, nil
   }
   return nil, errors.New("unsupported segment type")
}

func (m *media_file) New(represent *dash.Representation) error {
   for _, content := range represent.ContentProtection {
      if content.SchemeIdUri == widevine_urn {
         if content.Pssh != "" {
            data, err := base64.StdEncoding.DecodeString(content.Pssh)
            if err != nil {
               return err
            }
            var box pssh.Box
            n, err := box.BoxHeader.Decode(data)
            if err != nil {
               return err
            }
            err = box.Read(data[n:])
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

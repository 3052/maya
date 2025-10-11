package net

import (
   "41.neocities.org/dash"
   "41.neocities.org/sofia/file"
   "41.neocities.org/sofia/pssh"
   "encoding/base64"
   "errors"
   "fmt"
   "io"
   "log"
   "net/http"
   "net/url"
   "os"
   "slices"
   "strings"
   "time"
)

func (f Filters) Filter(resp *http.Response, config *WidevineConfig) error {
   if resp.StatusCode != http.StatusOK {
      var data strings.Builder
      resp.Write(&data)
      return errors.New(data.String())
   }
   defer resp.Body.Close()
   data, err := io.ReadAll(resp.Body)
   if err != nil {
      return err
   }
   var mpd dash.Mpd
   err = mpd.Unmarshal(data)
   if err != nil {
      return err
   }
   mpd.Set(resp.Request.URL)
   represents := slices.SortedFunc(mpd.Representation(),
      func(a, b *dash.Representation) int {
         return a.Bandwidth - b.Bandwidth
      },
   )
   for i, represent := range represents {
      if i >= 1 {
         fmt.Println()
      }
      fmt.Println(represent)
      if f.representation_ok(represent) {
         switch {
         case represent.SegmentBase != nil:
            err = config.segment_base(represent)
         case represent.SegmentList != nil:
            err = config.segment_list(represent)
         case represent.SegmentTemplate != nil:
            err = config.segment_template(represent)
         }
         if err != nil {
            return err
         }
      }
   }
   return nil
}

type Filters []Filter

func (f Filters) String() string {
   var b []byte
   for i, value := range f {
      if i >= 1 {
         b = append(b, ',')
      }
      b = fmt.Append(b, &value)
   }
   return string(b)
}

func (f *Filters) Set(data string) error {
   *f = nil
   for _, data := range strings.Split(data, ",") {
      var filterVar Filter
      err := filterVar.Set(data)
      if err != nil {
         return err
      }
      *f = append(*f, filterVar)
   }
   return nil
}

func (f *Filter) Set(data string) error {
   cookies, err := http.ParseCookie(data)
   if err != nil {
      return err
   }
   for _, cookie := range cookies {
      switch cookie.Name {
      case "bs":
         _, err = fmt.Sscan(cookie.Value, &f.BitrateStart)
      case "be":
         _, err = fmt.Sscan(cookie.Value, &f.BitrateEnd)
      case "i":
         f.Id = cookie.Value
      case "l":
         f.Language = cookie.Value
      case "r":
         f.Role = cookie.Value
      default:
         err = errors.New(".Name")
      }
      if err != nil {
         return err
      }
   }
   return nil
}

const FilterUsage = `be = bitrate end
bs = bitrate start
i = id
l = language
r = role`

func (f *Filter) String() string {
   var b []byte
   if f.BitrateStart >= 1 {
      b = fmt.Append(b, "bs=", f.BitrateStart)
   }
   if f.BitrateEnd >= 1 {
      if b != nil {
         b = append(b, ';')
      }
      b = fmt.Append(b, "be=", f.BitrateEnd)
   }
   if f.Id != "" {
      if b != nil {
         b = append(b, ';')
      }
      b = fmt.Append(b, "i=", f.Id)
   }
   if f.Language != "" {
      if b != nil {
         b = append(b, ';')
      }
      b = fmt.Append(b, "l=", f.Language)
   }
   if f.Role != "" {
      if b != nil {
         b = append(b, ';')
      }
      b = fmt.Append(b, "r=", f.Role)
   }
   return string(b)
}

type Filter struct {
   BitrateEnd   int
   BitrateStart int
   Id           string
   Language     string
   Role         string
}

func (f *Filter) bitrate_end_ok(rep *dash.Representation) bool {
   if f.BitrateEnd == 0 {
      return true
   }
   return rep.Bandwidth <= f.BitrateEnd
}

func (f *Filter) role_ok(rep *dash.Representation) bool {
   switch f.Role {
   case "", rep.GetAdaptationSet().GetRole():
      return true
   }
   return false
}

func (f *Filter) language_ok(rep *dash.Representation) bool {
   switch f.Language {
   case "", rep.GetAdaptationSet().Lang:
      return true
   }
   return false
}

func (f *Filter) id_ok(rep *dash.Representation) bool {
   switch f.Id {
   case "", rep.Id:
      return true
   }
   return false
}

func (f Filters) representation_ok(rep *dash.Representation) bool {
   for _, filterVar := range f {
      if rep.Bandwidth < filterVar.BitrateStart {
         continue
      }
      if !filterVar.bitrate_end_ok(rep) {
         continue
      }
      if !filterVar.id_ok(rep) {
         continue
      }
      if !filterVar.language_ok(rep) {
         continue
      }
      if !filterVar.role_ok(rep) {
         continue
      }
      return true
   }
   return false
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

const (
   widevine_system_id = "edef8ba979d64acea3c827dcd51d21ed"
   widevine_urn       = "urn:uuid:edef8ba9-79d6-4ace-a3c8-27dcd51d21ed"
)

var Threads = 1

func os_create(name string) (*os.File, error) {
   log.Println("Create", name)
   return os.Create(name)
}

func create(represent *dash.Representation) (*os.File, error) {
   var name strings.Builder
   name.WriteString(represent.Id)
   switch *represent.MimeType {
   case "audio/mp4":
      name.WriteString(".m4a")
   case "image/jpeg":
      name.WriteString(".jpg")
   case "video/mp4":
      name.WriteString(".m4v")
   }
   return os_create(name.String())
}

func get_segment(u *url.URL, head http.Header) ([]byte, error) {
   req := http.Request{Method: "GET", URL: u}
   if head != nil {
      req.Header = head
   } else {
      req.Header = http.Header{}
   }
   req.Header.Set("silent", "true")
   resp, err := http.DefaultClient.Do(&req)
   if err != nil {
      return nil, err
   }
   switch resp.StatusCode {
   case http.StatusOK, http.StatusPartialContent:
   default:
      var data strings.Builder
      resp.Write(&data)
      return nil, errors.New(data.String())
   }
   defer resp.Body.Close()
   return io.ReadAll(resp.Body)
}

func (i *index_range) Set(data string) error {
   _, err := fmt.Sscanf(data, "%v-%v", &i[0], &i[1])
   if err != nil {
      return err
   }
   return nil
}

type index_range [2]uint64

func (i *index_range) String() string {
   return fmt.Sprintf("%v-%v", i[0], i[1])
}

type media_file struct {
   key_id    []byte // tenc
   pssh      []byte // pssh
   timescale uint64 // mdhd
   size      uint64 // trun
   duration  uint64 // trun
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

func (p *progress) next() {
   p.segmentA++
   p.segmentB--
   timeB := time.Now().Unix()
   if timeB > p.timeB {
      log.Println(
         p.segmentB, "segment",
         p.durationB().Truncate(time.Second),
         "left",
      )
      p.timeB = timeB
   }
}

func (p *progress) set(segmentB int) {
   p.segmentB = segmentB
   p.timeA = time.Now()
   p.timeB = time.Now().Unix()
}

type progress struct {
   segmentA int64
   segmentB int
   timeA    time.Time
   timeB    int64
}

func (p *progress) durationA() time.Duration {
   return time.Since(p.timeA)
}

// keep last two terms separate
func (p *progress) durationB() time.Duration {
   return p.durationA() * time.Duration(p.segmentB) / time.Duration(p.segmentA)
}

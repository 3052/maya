package net

import (
   "41.neocities.org/dash"
   "41.neocities.org/drm/playReady"
   "41.neocities.org/drm/widevine"
   "41.neocities.org/sofia/file"
   "41.neocities.org/sofia/pssh"
   "bytes"
   "encoding/base64"
   "errors"
   "fmt"
   "io"
   "log"
   "math"
   "math/big"
   "net/http"
   "net/url"
   "os"
   "slices"
   "strings"
   "time"
)

func (f *Filter) index(streams []*dash.Representation) int {
   const penalty_factor = 2
   min_score := math.MaxInt
   best_stream := -1
   for i, candidate := range streams {
      if f.Codecs != "" {
         if candidate.Codecs != nil {
            if !strings.HasPrefix(*candidate.Codecs, f.Codecs) {
               continue
            }
         }
      }
      if f.Height >= 1 {
         if candidate.Height != nil {
            if *candidate.Height != f.Height {
               continue
            }
         }
      }
      if f.Id != "" {
         if candidate.Id == f.Id {
            return i
         } else {
            continue
         }
      }
      if f.Lang != "" {
         if candidate.GetAdaptationSet().Lang != f.Lang {
            continue
         }
      }
      if f.Role != "" {
         if candidate.GetAdaptationSet().GetRole() != f.Role {
            continue
         }
      }
      var score int
      if candidate.Bandwidth >= f.Bandwidth {
         score = candidate.Bandwidth - f.Bandwidth
      } else {
         score = (f.Bandwidth - candidate.Bandwidth) * penalty_factor
      }
      if score < min_score {
         min_score = score
         best_stream = i
      }
   }
   return best_stream
}

func (f *Filters) Filter(resp *http.Response, configVar *Config) error {
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
   }
   for _, target := range f.Values {
      index := target.index(represents)
      if index == -1 {
         continue
      }
      represent := represents[index]
      switch {
      case represent.SegmentBase != nil:
         err = configVar.segment_base(represent)
      case represent.SegmentList != nil:
         err = configVar.segment_list(represent)
      case represent.SegmentTemplate != nil:
         err = configVar.segment_template(represent)
      }
      if err != nil {
         return err
      }
   }
   return nil
}

type Filters struct {
   Values []Filter
   set    bool
}

func (f *Filters) Set(input string) error {
   if !f.set {
      f.Values = nil
      f.set = true
   }
   var value Filter
   err := value.Set(input)
   if err != nil {
      return err
   }
   f.Values = append(f.Values, value)
   return nil
}

func (f *Filters) String() string {
   var out []byte
   for i, value := range f.Values {
      if i >= 1 {
         out = append(out, ' ')
      }
      out = fmt.Append(out, "-f ", &value)
   }
   return string(out)
}

func (f *Filter) Set(input string) error {
   for _, pair := range strings.Split(input, ",") {
      key, value, found := strings.Cut(pair, "=")
      if !found {
         return errors.New("invalid pair format")
      }
      var err error
      switch key {
      case "b":
         _, err = fmt.Sscan(value, &f.Bandwidth)
      case "c":
         f.Codecs = value
      case "h":
         _, err = fmt.Sscan(value, &f.Height)
      case "i":
         f.Id = value
      case "l":
         f.Lang = value
      case "r":
         f.Role = value
      default:
         err = errors.New("unknown key")
      }
      if err != nil {
         return err
      }
   }
   return nil
}

func (f *Filter) String() string {
   var out []byte
   if f.Bandwidth >= 1 {
      out = fmt.Append(out, "b=", f.Bandwidth)
   }
   if f.Codecs != "" {
      if out != nil {
         out = append(out, ',')
      }
      out = fmt.Append(out, "c=", f.Codecs)
   }
   if f.Height >= 1 {
      if out != nil {
         out = append(out, ',')
      }
      out = fmt.Append(out, "h=", f.Height)
   }
   if f.Id != "" {
      if out != nil {
         out = append(out, ',')
      }
      out = fmt.Append(out, "i=", f.Id)
   }
   if f.Lang != "" {
      if out != nil {
         out = append(out, ',')
      }
      out = fmt.Append(out, "l=", f.Lang)
   }
   if f.Role != "" {
      if out != nil {
         out = append(out, ',')
      }
      out = fmt.Append(out, "r=", f.Role)
   }
   return string(out)
}

const FilterUsage = `b = bandwidth
c = codecs
h = height
i = id
l = lang
r = role`

type Filter struct {
   Bandwidth int
   Id        string
   Height    int
   Lang      string
   Role      string
   Codecs    string
}

func (c *Config) segment_list(represent *dash.Representation) error {
   if Threads != 1 {
      return errors.New("SegmentList Threads")
   }
   var media media_file
   err := media.New(represent)
   if err != nil {
      return err
   }
   fileVar, err := create(represent)
   if err != nil {
      return err
   }
   defer fileVar.Close()
   data, err := get_segment(
      represent.SegmentList.Initialization.SourceUrl[0], nil,
   )
   if err != nil {
      return err
   }
   data, err = media.initialization(data)
   if err != nil {
      return err
   }
   _, err = fileVar.Write(data)
   if err != nil {
      return err
   }
   key, err := c.widevine_key(&media)
   if err != nil {
      return err
   }
   var progressVar progress
   progressVar.set(len(represent.SegmentList.SegmentUrl))
   for _, segment := range represent.SegmentList.SegmentUrl {
      data, err := get_segment(segment.Media[0], nil)
      if err != nil {
         return err
      }
      progressVar.next()
      data, err = media.write_segment(data, key)
      if err != nil {
         return err
      }
      _, err = fileVar.Write(data)
      if err != nil {
         return err
      }
   }
   return nil
}

type Config struct {
   Send func([]byte) ([]byte, error)
   // PlayReady
   CertificateChain string
   EncryptSignKey   string
   // Widevine
   ClientId   string
   PrivateKey string
}

func (c *Config) widevine_key(media *media_file) ([]byte, error) {
   if media.key_id == nil {
      return nil, nil
   }
   private_key, err := os.ReadFile(c.PrivateKey)
   if err != nil {
      return nil, err
   }
   client_id, err := os.ReadFile(c.ClientId)
   if err != nil {
      return nil, err
   }
   if media.pssh == nil {
      var psshVar widevine.Pssh
      psshVar.KeyIds = [][]byte{media.key_id}
      media.pssh = psshVar.Marshal()
   }
   log.Println("PSSH", base64.StdEncoding.EncodeToString(media.pssh))
   var cdm widevine.Cdm
   err = cdm.New(private_key, client_id, media.pssh)
   if err != nil {
      return nil, err
   }
   data, err := cdm.RequestBody()
   if err != nil {
      return nil, err
   }
   data, err = c.Send(data)
   if err != nil {
      return nil, err
   }
   var body widevine.ResponseBody
   err = body.Unmarshal(data)
   if err != nil {
      return nil, err
   }
   block, err := cdm.Block(body)
   if err != nil {
      return nil, err
   }
   for container := range body.Container() {
      if bytes.Equal(container.Id(), media.key_id) {
         key := container.Key(block)
         log.Printf("key %x", key)
         var zero [16]byte
         if !bytes.Equal(key, zero[:]) {
            return key, nil
         }
      }
   }
   return nil, errors.New("widevine_key")
}

func (c *Config) segment_template(represent *dash.Representation) error {
   var media media_file
   err := media.New(represent)
   if err != nil {
      return err
   }
   fileVar, err := create(represent)
   if err != nil {
      return err
   }
   defer fileVar.Close()
   if initial := represent.SegmentTemplate.Initialization; initial != "" {
      address, err := initial.Url(represent)
      if err != nil {
         return err
      }
      data1, err := get_segment(address, nil)
      if err != nil {
         return err
      }
      data1, err = media.initialization(data1)
      if err != nil {
         return err
      }
      _, err = fileVar.Write(data1)
      if err != nil {
         return err
      }
   }
   key, err := c.widevine_key(&media)
   if err != nil {
      return err
   }
   var segments []int
   for rep := range represent.Representation() {
      segments = slices.AppendSeq(segments, rep.Segment())
   }
   var progressVar progress
   progressVar.set(len(segments))
   for chunk := range slices.Chunk(segments, Threads) {
      var (
         datas = make([][]byte, len(chunk))
         errs  = make(chan error)
      )
      for i, segment := range chunk {
         address, err := represent.SegmentTemplate.Media.Url(represent, segment)
         if err != nil {
            return err
         }
         go func() {
            datas[i], err = get_segment(address, nil)
            errs <- err
            progressVar.next()
         }()
      }
      for range chunk {
         err := <-errs
         if err != nil {
            return err
         }
      }
      for _, data := range datas {
         data, err = media.write_segment(data, key)
         if err != nil {
            return err
         }
         _, err = fileVar.Write(data)
         if err != nil {
            return err
         }
      }
   }
   return nil
}

func (c *Config) playReady_key(media *media_file) ([]byte, error) {
   data, err := os.ReadFile(c.CertificateChain)
   if err != nil {
      return nil, err
   }
   var chain playReady.Chain
   err = chain.Decode(data)
   if err != nil {
      return nil, err
   }
   data, err = os.ReadFile(c.EncryptSignKey)
   if err != nil {
      return nil, err
   }
   encryptSignKey := new(big.Int).SetBytes(data)
   log.Printf("key ID %x", media.key_id)
   playReady.UuidOrGuid(media.key_id)
   data, err = chain.RequestBody(media.key_id, encryptSignKey)
   if err != nil {
      return nil, err
   }
   data, err = c.Send(data)
   if err != nil {
      return nil, err
   }
   var license playReady.License
   coord, err := license.Decrypt(data, encryptSignKey)
   if err != nil {
      return nil, err
   }
   if !bytes.Equal(license.ContentKey.KeyId[:], media.key_id) {
      return nil, errors.New("key ID mismatch")
   }
   key := coord.Key()
   log.Printf("key %x", key)
   return key, nil
}

func (c *Config) key(media *media_file) ([]byte, error) {
   if c.CertificateChain != "" {
      if c.EncryptSignKey != "" {
         return c.playReady_key(media)
      }
   }
   return c.widevine_key(media)
}

func create(represent *dash.Representation) (*os.File, error) {
   var name strings.Builder
   name.WriteString(represent.Id)
   switch *represent.MimeType {
   case "audio/mp4":
      name.WriteString(".m4a")
   case "text/vtt":
      name.WriteString(".vtt")
   case "video/mp4":
      name.WriteString(".m4v")
   }
   return os_create(name.String())
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

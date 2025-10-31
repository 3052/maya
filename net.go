package net

import (
   "41.neocities.org/dash"
   "41.neocities.org/drm/playReady"
   "41.neocities.org/sofia"
   "bytes"
   "encoding/hex"
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
      err = configVar.DownloadRepresentation(represent)
      if err != nil {
         return err
      }
   }
   return nil
}

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
type media_file struct {
   key_id    []byte // tenc
   pssh      []byte // pssh
   timescale uint64 // mdhd
   size      uint64 // trun
   duration  uint64 // trun
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

func (c *Config) DownloadRepresentation(represent *dash.Representation) error {
   var media media_file
   if err := media.New(represent); err != nil {
      return err
   }
   fileVar, err := create(represent)
   if err != nil {
      return err
   }
   defer fileVar.Close()

   if err := c.download_initialization(represent, &media, fileVar); err != nil {
      return err
   }

   key, err := c.key(&media)
   if err != nil {
      return err
   }

   requests, err := c.get_media_requests(represent)
   if err != nil {
      return err
   }

   if len(requests) == 0 {
      return nil
   }

   numWorkers := c.Threads
   if numWorkers < 1 {
      numWorkers = 1
   }
   jobs := make(chan job, len(requests))
   results := make(chan result, len(requests))
   doneChan := make(chan error, 1)

   // Define the writer's logic in a clearly named variable.
   writerFunc := func() error {
      pending := make(map[int][]byte)
      nextIndex := 0
      var progressVar progress
      progressVar.set(len(requests))

      for i := 0; i < len(requests); i++ {
         res := <-results
         if res.err != nil {
            return res.err
         }

         pending[res.index] = res.data
         progressVar.next()

         for data, ok := pending[nextIndex]; ok; data, ok = pending[nextIndex] {
            processedData, err := media.write_segment(data, key)
            if err != nil {
               return err
            }
            if _, err = fileVar.Write(processedData); err != nil {
               return err
            }
            delete(pending, nextIndex)
            nextIndex++
         }
      }
      return nil
   }

   // Launch the writer goroutine. Its purpose is now much clearer.
   go func() {
      doneChan <- writerFunc()
   }()

   // Start worker goroutines
   for w := 0; w < numWorkers; w++ {
      go func() {
         for j := range jobs {
            data, err := get_segment(j.request.url, j.request.header)
            results <- result{index: j.index, data: data, err: err}
         }
      }()
   }

   // Send all jobs
   for i, req := range requests {
      jobs <- job{index: i, request: req}
   }
   close(jobs)

   // Block and wait for the final status from the writer.
   return <-doneChan
}

type media_request struct {
   url    *url.URL
   header http.Header
}

func (c *Config) download_initialization(
   represent *dash.Representation, media *media_file, fileVar *os.File,
) error {
   var (
      data []byte
      err  error
   )
   switch {
   case represent.SegmentList != nil:
      data, err = get_segment(represent.SegmentList.Initialization.SourceUrl[0], nil)

   case represent.SegmentTemplate != nil && represent.SegmentTemplate.Initialization != "":
      address, urlErr := represent.SegmentTemplate.Initialization.Url(represent)
      if urlErr != nil {
         return urlErr
      }
      data, err = get_segment(address, nil)

   case represent.SegmentBase != nil:
      head := http.Header{}
      head.Set("range", "bytes="+represent.SegmentBase.Initialization.Range)
      data, err = get_segment(represent.BaseUrl[0], head)

   default:
      // No initialization segment to download
      return nil
   }

   if err != nil {
      return err
   }
   data, err = media.initialization(data)
   if err != nil {
      return err
   }
   _, err = fileVar.Write(data)
   return err
}

func os_create(name string) (*os.File, error) {
   log.Println("Create", name)
   return os.Create(name)
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

func (p *progress) durationA() time.Duration {
   return time.Since(p.timeA)
}

// keep last two terms separate
func (p *progress) durationB() time.Duration {
   return p.durationA() * time.Duration(p.segmentB) / time.Duration(p.segmentA)
}

func (i *index_range) Set(data string) error {
   _, err := fmt.Sscanf(data, "%v-%v", &i[0], &i[1])
   if err != nil {
      return err
   }
   return nil
}

func (i *index_range) String() string {
   return fmt.Sprintf("%v-%v", i[0], i[1])
}

type index_range [2]uint64

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

func get_segment(u *url.URL, head http.Header) ([]byte, error) {
   req := http.Request{Method: "GET", URL: u}
   if head != nil {
      req.Header = head
   } else {
      req.Header = http.Header{}
   }
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

type Config struct {
   Send func([]byte) ([]byte, error)
   // Number of segments to download in parallel
   Threads int
   // PlayReady
   CertificateChain string
   EncryptSignKey   string
   // Widevine
   ClientId   string
   PrivateKey string
}

type job struct {
   index   int
   request media_request
}

type result struct {
   index int
   data  []byte
   err   error
}

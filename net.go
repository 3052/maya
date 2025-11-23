package net

import (
   "41.neocities.org/dash"
   "41.neocities.org/drm/playReady"
   "41.neocities.org/drm/widevine"
   "41.neocities.org/sofia"
   "bytes"
   "encoding/base64"
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

func (c *Config) download_initialization(
   file *os.File,
   media *media_file,
   represent *dash.Representation,
) error {
   template := represent.GetSegmentTemplate()
   var data []byte
   switch {
   case represent.SegmentBase != nil:
      head := http.Header{}
      head.Set("range", "bytes="+represent.SegmentBase.Initialization.Range)
      address, err := represent.ResolveBaseURL()
      if err != nil {
         return err
      }
      data, err = get_segment(address, head)
      if err != nil {
         return err
      }
   case template != nil && template.Initialization != "":
      address, err := template.ResolveInitialization(represent)
      if err != nil {
         return err
      }
      data, err = get_segment(address, nil)
      if err != nil {
         return err
      }
   case represent.SegmentList != nil:
      source_url, err := represent.SegmentList.Initialization.ResolveSourceURL()
      if err != nil {
         return err
      }
      data, err = get_segment(source_url, nil)
      if err != nil {
         return err
      }
   default:
      // No initialization segment to download
      return nil
   }
   var err error
   data, err = media.initialization(data)
   if err != nil {
      return err
   }
   _, err = file.Write(data)
   if err != nil {
      return err
   }
   return nil
}

func create(represent *dash.Representation) (*os.File, error) {
   var name strings.Builder
   name.WriteString(represent.ID)
   switch represent.GetMimeType() {
   case "audio/mp4":
      name.WriteString(".m4a")
   case "text/vtt":
      name.WriteString(".vtt")
   case "video/mp4":
      name.WriteString(".m4v")
   }
   return os_create(name.String())
}
func (f *Filter) index(groups [][]*dash.Representation) int {
   const penalty_factor = 2
   min_score := math.MaxInt
   best_stream := -1
   for i, group := range groups {
      represent := group[0]
      if f.Codecs != "" {
         if !strings.HasPrefix(represent.GetCodecs(), f.Codecs) {
            continue
         }
      }
      if f.Height >= 1 {
         if represent.GetHeight() != f.Height {
            continue
         }
      }
      if f.Id != "" {
         if represent.ID == f.Id {
            return i
         } else {
            continue
         }
      }
      if f.Lang != "" {
         if represent.Parent.Lang != f.Lang {
            continue
         }
      }
      if f.Role != "" {
         if represent.Parent.Role == nil {
            continue
         }
         if represent.Parent.Role.Value != f.Role {
            continue
         }
      }
      var score int
      if represent.Bandwidth >= f.Bandwidth {
         score = represent.Bandwidth - f.Bandwidth
      } else {
         score = (f.Bandwidth - represent.Bandwidth) * penalty_factor
      }
      if score < min_score {
         min_score = score
         best_stream = i
      }
   }
   return best_stream
}
func (m *media_file) New(represent *dash.Representation) error {
   for protect := range represent.GetContentProtection() {
      if protect.SchemeIdUri == widevine_urn {
         if protect.Pssh != "" {
            data, err := base64.StdEncoding.DecodeString(protect.Pssh)
            if err != nil {
               return err
            }
            var box sofia.PsshBox
            err = box.Parse(data)
            if err != nil {
               return err
            }
            m.pssh = box.Data
            log.Println("MPD PSSH", base64.StdEncoding.EncodeToString(m.pssh))
            break
         }
      }
   }
   return nil
}

func (c *Config) download(group []*dash.Representation) error {
   represent := group[0]
   var media media_file
   if err := media.New(represent); err != nil {
      return err
   }
   file, err := create(represent)
   if err != nil {
      return err
   }
   defer file.Close()
   if err := c.download_initialization(file, &media, represent); err != nil {
      return err
   }
   key, err := c.key(&media)
   if err != nil {
      return err
   }
   requests, err := c.get_media_requests(group)
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
   // Launch the writer goroutine as a method on our media_file instance.
   // This is much cleaner than the previous closure.
   go media.processAndWriteSegments(doneChan, results, len(requests), key, file)
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

func (c *Config) get_media_requests(group []*dash.Representation) ([]media_request, error) {
   represent := group[0]
   base_url, err := represent.ResolveBaseURL()
   if err != nil {
      return nil, err
   }
   template := represent.GetSegmentTemplate()
   switch {
   case template != nil:
      var requests []media_request
      for _, represent := range group {
         addresses, err := template.GetSegmentURLs(represent)
         if err != nil {
            return nil, err
         }
         for _, address := range addresses {
            requests = append(requests, media_request{url: address})
         }
      }
      return requests, nil
   case represent.SegmentList != nil:
      var requests []media_request
      for _, segment := range represent.SegmentList.SegmentURLs {
         address, err := segment.ResolveMedia()
         if err != nil {
            return nil, err
         }
         requests = append(requests, media_request{url: address})
      }
      return requests, nil
   case represent.SegmentBase != nil:
      head := http.Header{}
      head.Set("range", "bytes="+represent.SegmentBase.IndexRange)
      data, err := get_segment(base_url, head)
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
         return nil, sofia.Missing("sidx")
      }
      requests := make([]media_request, len(sidx.References))
      for i, reference := range sidx.References {
         index[0] = index[1] + 1
         index[1] += uint64(reference.ReferencedSize)
         range_head := http.Header{}
         range_head.Set("range", "bytes="+index.String())
         requests[i] = media_request{url: base_url, header: range_head}
      }
      return requests, nil
   }
   return []media_request{{url: base_url}}, nil
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
   mpd, err := dash.Parse(data)
   if err != nil {
      return err
   }
   mpd.MPDURL = resp.Request.URL
   var groups [][]*dash.Representation
   for _, group := range mpd.GetRepresentations() {
      groups = append(groups, group)
   }
   slices.SortFunc(groups, func(a, b []*dash.Representation) int {
      return a[0].Bandwidth - b[0].Bandwidth
   })
   for i, group := range groups {
      if i >= 1 {
         fmt.Println()
      }
      fmt.Println(group[0])
   }
   for _, target := range f.Values {
      index := target.index(groups)
      if index == -1 {
         continue
      }
      group := groups[index]
      err = configVar.download(group)
      if err != nil {
         return err
      }
   }
   return nil
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

func (c *Config) key(media *media_file) ([]byte, error) {
   if media.key_id == nil {
      return nil, nil
   }
   if c.CertificateChain != "" {
      if c.EncryptSignKey != "" {
         return c.playReady_key(media)
      }
   }
   return c.widevine_key(media)
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

func (c *Config) widevine_key(media *media_file) ([]byte, error) {
   client_id, err := os.ReadFile(c.ClientId)
   if err != nil {
      return nil, err
   }
   pem_bytes, err := os.ReadFile(c.PrivateKey)
   if err != nil {
      return nil, err
   }
   req_bytes, err := widevine.BuildLicenseRequest(client_id, media.pssh, 1)
   if err != nil {
      return nil, err
   }
   private_key, err := widevine.ParsePrivateKey(pem_bytes)
   if err != nil {
      return nil, err
   }
   signed_bytes, err := widevine.BuildSignedMessage(req_bytes, private_key)
   if err != nil {
      return nil, err
   }
   signed_bytes, err = c.Send(signed_bytes)
   if err != nil {
      return nil, err
   }
   keys, err := widevine.ParseLicenseResponse(
      signed_bytes, req_bytes, private_key,
   )
   if err != nil {
      return nil, err
   }
   found_key, ok := widevine.GetKey(keys, media.key_id)
   if !ok {
      return nil, errors.New("GetKey: key not found in response")
   }
   var zero [16]byte
   if bytes.Equal(found_key, zero[:]) {
      return nil, errors.New("zero key")
   }
   log.Printf("key %x", found_key)
   return found_key, nil
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

var widevine_id, _ = hex.DecodeString("edef8ba979d64acea3c827dcd51d21ed")

// EDTS FIXES A/V SYNC WITH
// rakuten.tv
// BUT MIGHT BREAK THESE
// criterionchannel.com
// mubi.com
// paramountplus.com
// tubitv.com
func (m *media_file) initialization(data []byte) ([]byte, error) {
   parsedInit, err := sofia.Parse(data)
   if err != nil {
      return nil, err
   }
   moov, ok := sofia.FindMoov(parsedInit)
   if !ok {
      return nil, sofia.Missing("moov")
   }
   if m.pssh == nil {
      widevine_box, ok := moov.FindPssh(widevine_id)
      if ok {
         m.pssh = widevine_box.Data
         log.Println("MP4 PSSH", base64.StdEncoding.EncodeToString(m.pssh))
      }
   }
   trak, ok := moov.Trak()
   if !ok {
      return nil, sofia.Missing("trak")
   }
   trak.ReplaceEdts()
   mdia, ok := trak.Mdia()
   if !ok {
      return nil, sofia.Missing("mdia")
   }
   mdhd, ok := mdia.Mdhd()
   if !ok {
      return nil, sofia.Missing("mdhd")
   }
   m.timescale = uint64(mdhd.Timescale)
   minf, ok := mdia.Minf()
   if !ok {
      return nil, sofia.Missing("minf")
   }
   stbl, ok := minf.Stbl()
   if !ok {
      return nil, sofia.Missing("stbl")
   }
   stsd, ok := stbl.Stsd()
   if !ok {
      return nil, sofia.Missing("stsd")
   }
   sinf, _, ok := stsd.Sinf()
   if ok {
      schi, ok := sinf.Schi()
      if !ok {
         return nil, sofia.Missing("schi")
      }
      tenc, ok := schi.Tenc()
      if !ok {
         return nil, sofia.Missing("tenc")
      }
      m.key_id = tenc.DefaultKID[:]
   }
   err = moov.Sanitize()
   if err != nil {
      return nil, err
   }
   var finalMP4Data bytes.Buffer
   for _, box := range parsedInit {
      finalMP4Data.Write(box.Encode())
   }
   return finalMP4Data.Bytes(), nil
}

func os_create(name string) (*os.File, error) {
   log.Println("Create", name)
   return os.Create(name)
}

const widevine_urn = "urn:uuid:edef8ba9-79d6-4ace-a3c8-27dcd51d21ed"

type media_request struct {
   url    *url.URL
   header http.Header
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

// segment can be VTT or anything
func (m *media_file) write_segment(data, key []byte) ([]byte, error) {
   if key == nil {
      return data, nil
   }
   parsedSegment, err := sofia.Parse(data)
   if err != nil {
      return nil, err
   }
   for _, moof := range sofia.AllMoof(parsedSegment) {
      traf, ok := moof.Traf()
      if !ok {
         return nil, sofia.Missing("traf")
      }
      total_bytes, total_duration, err := traf.Totals()
      if err != nil {
         return nil, err
      }
      m.size += total_bytes
      m.duration += total_duration
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

func (m *media_file) processAndWriteSegments(
   doneChan chan<- error,
   results <-chan result,
   totalSegments int,
   key []byte,
   fileVar *os.File,
) {
   pending := make(map[int][]byte)
   nextIndex := 0
   var progressVar progress
   progressVar.set(totalSegments)

   for i := 0; i < totalSegments; i++ {
      res := <-results
      if res.err != nil {
         doneChan <- res.err
         return
      }

      pending[res.index] = res.data

      for data, ok := pending[nextIndex]; ok; data, ok = pending[nextIndex] {
         processedData, err := m.write_segment(data, key)
         if err != nil {
            doneChan <- err
            return
         }
         if _, err = fileVar.Write(processedData); err != nil {
            doneChan <- err
            return
         }
         delete(pending, nextIndex)
         nextIndex++

         progressVar.next()
         timeB := time.Now().Unix()
         if timeB > progressVar.timeB {
            var bandwidth uint64
            if m.duration > 0 { // Avoid division by zero
               bandwidth = m.size * 8 * m.timescale / m.duration
            }
            log.Printf(
               "done %d | left %d | ETA %s | %d bps",
               progressVar.segmentA,
               progressVar.segmentB,
               progressVar.durationB().Truncate(time.Second),
               bandwidth,
            )
            progressVar.timeB = timeB
         }
      }
   }
   doneChan <- nil
}

// keep last two terms separate
func (p *progress) durationB() time.Duration {
   return p.durationA() * time.Duration(p.segmentB) / time.Duration(p.segmentA)
}

func (p *progress) durationA() time.Duration {
   return time.Since(p.timeA)
}

func (p *progress) set(segmentB int) {
   p.segmentB = segmentB
   p.timeA = time.Now()
   p.timeB = time.Now().Unix()
}

func (p *progress) next() {
   p.segmentA++
   p.segmentB--
}

type media_file struct {
   key_id    []byte // tenc
   pssh      []byte // pssh
   timescale uint64 // mdhd
   size      uint64 // trun
   duration  uint64 // trun
}

type progress struct {
   segmentA int64
   segmentB int
   timeA    time.Time
   timeB    int64
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

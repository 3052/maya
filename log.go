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
   "math/big"
   "net/http"
   "net/url"
   "os"
   "slices"
   "strings"
   "time"
)

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

// processAndWriteSegments handles the sequential writing of downloaded segments.
// It runs in its own goroutine, processing results from the workers,
// writing them to a file in the correct order, and logging progress.
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

            // Final format: "bandwidth" word removed
            log.Printf(
               "processed %d | remaining %d | ETA %s | %d bps",
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

func (p *progress) next() {
   p.segmentA++
   p.segmentB--
}

func (c *Config) Download(represent *dash.Representation) error {
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

   // Launch the writer goroutine as a method on our media_file instance.
   // This is much cleaner than the previous closure.
   go media.processAndWriteSegments(doneChan, results, len(requests), key, fileVar)

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

func (p *progress) durationA() time.Duration {
   return time.Since(p.timeA)
}

// keep last two terms separate
func (p *progress) durationB() time.Duration {
   return p.durationA() * time.Duration(p.segmentB) / time.Duration(p.segmentA)
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

func (p *progress) set(segmentB int) {
   p.segmentB = segmentB
   p.timeA = time.Now()
   p.timeB = time.Now().Unix()
}

func (c *Config) get_media_requests(represent *dash.Representation) ([]media_request, error) {
   switch {
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
   case represent.SegmentList != nil:
      requests := make([]media_request, len(represent.SegmentList.SegmentUrl))
      for i, segment := range represent.SegmentList.SegmentUrl {
         requests[i] = media_request{url: segment.Media[0]}
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
         return nil, sofia.Missing("sidx")
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
   return []media_request{
      {url: represent.BaseUrl[0]},
   }, nil
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

const widevine_urn = "urn:uuid:edef8ba9-79d6-4ace-a3c8-27dcd51d21ed"

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
            log.Println("MPD PSSH", base64.StdEncoding.EncodeToString(m.pssh))
            break
         }
      }
   }
   return nil
}

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

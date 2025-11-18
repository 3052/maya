package net

import (
   "41.neocities.org/dash"
   "41.neocities.org/sofia"
   "encoding/base64"
   "errors"
   "fmt"
   "io"
   "log"
   "math"
   "net/http"
   "os"
   "slices"
   "strings"
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
      err = configVar.Download(represent)
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

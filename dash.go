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

func (c *Config) download_initialization(
   represent *dash.Representation, media *media_file, file *os.File,
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
   if err := c.download_initialization(represent, &media, file); err != nil {
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

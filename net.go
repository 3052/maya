package net

import (
   "41.neocities.org/dash"
   "errors"
   "fmt"
   "io"
   "math"
   "net/http"
   "slices"
   "strings"
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

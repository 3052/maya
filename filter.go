package net

import (
   "41.neocities.org/dash"
   "errors"
   "fmt"
   "io"
   "iter"
   "math"
   "net/http"
   "strings"
)

func find(
   streams iter.Seq[*dash.Representation], target *Filter,
) *dash.Representation {
   const penalty_factor = 2
   min_score := math.MaxInt
   var best_stream *dash.Representation
   for candidate := range streams {
      if target.Codecs != "" {
         if !strings.HasPrefix(*candidate.Codecs, target.Codecs) {
            continue
         }
      }
      if target.Height >= 1 {
         if *candidate.Height != target.Height {
            continue
         }
      }
      if candidate.Id == target.Id {
         return candidate
      }
      if target.Lang != "" {
         if candidate.GetAdaptationSet().Lang != target.Lang {
            continue
         }
      }
      if target.Role != "" {
         if candidate.GetAdaptationSet().GetRole() != target.Role {
            continue
         }
      }
      var score int
      if candidate.Bandwidth >= target.Bandwidth {
         score = candidate.Bandwidth - target.Bandwidth
      } else {
         score = (target.Bandwidth - candidate.Bandwidth) * penalty_factor
      }
      if score < min_score {
         min_score = score
         best_stream = candidate
      }
   }
   return best_stream
}

type Filter struct {
   Bandwidth int
   Id        string
   Height    int
   Lang      string
   Role      string
   Codecs    string
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
   for _, target := range f.Values {
      represent := find(mpd.Representation(), &target)
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

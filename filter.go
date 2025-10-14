package net

import (
   "41.neocities.org/dash"
   "errors"
   "fmt"
   "io"
   "net/http"
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
      if f.representation_ok(represent) {
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
   }
   return nil
}

func (f *Filter) id_ok(rep *dash.Representation) bool {
   switch f.Id {
   case "", rep.Id:
      return true
   }
   return false
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

func (f *Filter) role_ok(rep *dash.Representation) bool {
   switch f.Role {
   case "", rep.GetAdaptationSet().GetRole():
      return true
   }
   return false
}

func (f *Filter) lang_ok(rep *dash.Representation) bool {
   switch f.Lang {
   case "", rep.GetAdaptationSet().Lang:
      return true
   }
   return false
}

func (f *Filter) height_ok(rep *dash.Representation) bool {
   switch f.Height {
   case 0, *rep.Height:
      return true
   }
   return false
}

type Filters struct {
   Values []Filter
   set    bool
}

func (f *Filter) codecs_ok(rep *dash.Representation) bool {
   if f.Codecs == "" {
      return true
   }
   if rep.Codecs != nil {
      if strings.HasPrefix(*rep.Codecs, f.Codecs) {
         return true
      }
   }
   return false
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

///

type Filter struct {
   Codecs       string
   Height       int
   Id           string
   Lang         string
   MaxBandwidth int
   MinBandwidth int
   Role         string
}

const FilterUsage = `B = max bandwidth
b = min bandwidth
c = codecs
h = height
i = id
l = lang
r = role`

func (f *Filter) Set(input string) error {
   for _, pair := range strings.Split(input, ",") {
      key, value, found := strings.Cut(pair, "=")
      if !found {
         return errors.New("invalid pair format")
      }
      var err error
      switch key {
      case "B":
         _, err = fmt.Sscan(value, &f.MaxBandwidth)
      case "b":
         _, err = fmt.Sscan(value, &f.MinBandwidth)
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

func (f *Filter) max_bitrate_ok(rep *dash.Representation) bool {
   if f.MaxBandwidth == 0 {
      return true
   }
   return rep.Bandwidth <= f.MaxBandwidth
}

func (f *Filters) representation_ok(rep *dash.Representation) bool {
   for _, value := range f.Values {
      if !value.codecs_ok(rep) {
         continue
      }
      if !value.height_ok(rep) {
         continue
      }
      if !value.id_ok(rep) {
         continue
      }
      if !value.lang_ok(rep) {
         continue
      }
      if !value.max_bitrate_ok(rep) {
         continue
      }
      if value.MinBandwidth > rep.Bandwidth {
         continue
      }
      if !value.role_ok(rep) {
         continue
      }
      return true
   }
   return false
}

func (f *Filter) String() string {
   var out []byte
   if f.Codecs != "" {
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
   if f.MaxBandwidth >= 1 {
      if out != nil {
         out = append(out, ',')
      }
      out = fmt.Append(out, "B=", f.MaxBandwidth)
   }
   if f.MinBandwidth >= 1 {
      if out != nil {
         out = append(out, ',')
      }
      out = fmt.Append(out, "b=", f.MinBandwidth)
   }
   if f.Role != "" {
      if out != nil {
         out = append(out, ',')
      }
      out = fmt.Append(out, "r=", f.Role)
   }
   return string(out)
}

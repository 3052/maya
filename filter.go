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

func (f Filters) Filter(resp *http.Response, configVar *Config) error {
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

const FilterUsage = `be = bitrate end
bs = bitrate start
h = height
i = id
l = language
r = role`

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
      case "h":
         _, err = fmt.Sscan(cookie.Value, &f.Height)
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
   if f.Height >= 1 {
      if b != nil {
         b = append(b, ';')
      }
      b = fmt.Append(b, "h=", f.Height)
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
   Height       int
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

func (f *Filter) height_ok(rep *dash.Representation) bool {
   switch f.Height {
   case 0, *rep.Height:
      return true
   }
   return false
}

func (f Filters) representation_ok(rep *dash.Representation) bool {
   for _, filterVar := range f {
      if !filterVar.bitrate_end_ok(rep) {
         continue
      }
      if filterVar.BitrateStart > rep.Bandwidth {
         continue
      }
      if !filterVar.height_ok(rep) {
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

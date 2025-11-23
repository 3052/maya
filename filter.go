package net

import (
   "errors"
   "fmt"
   "io"
   "math"
   "net/http"
   "slices"
   "strings"

   "41.neocities.org/dash"
)

type Filters struct {
   Values []Filter
   set    bool
}

type Filter struct {
   Bandwidth int
   Height    int
   Id        string
   Lang      string
   Role      string
   Codecs    string
}

func (f *Filters) Filter(response *http.Response, configVar *Config) error {
   if response.StatusCode != http.StatusOK {
      var data strings.Builder
      response.Write(&data)
      return errors.New(data.String())
   }
   defer response.Body.Close()

   data, err := io.ReadAll(response.Body)
   if err != nil {
      return err
   }

   mpd, err := dash.Parse(data)
   if err != nil {
      return err
   }
   mpd.MPDURL = response.Request.URL

   var groups [][]*dash.Representation
   for _, group := range mpd.GetRepresentations() {
      groups = append(groups, group)
   }

   slices.SortFunc(groups, func(groupA, groupB []*dash.Representation) int {
      return groupA[0].Bandwidth - groupB[0].Bandwidth
   })

   for groupIndex, group := range groups {
      if groupIndex >= 1 {
         fmt.Println()
      }
      fmt.Println(group[0])
   }

   for _, target := range f.Values {
      bestIndex := target.index(groups)
      if bestIndex == -1 {
         continue
      }

      group := groups[bestIndex]
      if err := configVar.Download(group); err != nil {
         return err
      }
   }
   return nil
}

func (f *Filters) Set(input string) error {
   if !f.set {
      f.Values = nil
      f.set = true
   }
   var value Filter
   if err := value.Set(input); err != nil {
      return err
   }
   f.Values = append(f.Values, value)
   return nil
}

func (f *Filters) String() string {
   var parts []string
   for _, filter := range f.Values {
      parts = append(parts, "-f "+filter.String())
   }
   return strings.Join(parts, " ")
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
         err = errors.New("unknown key: " + key)
      }
      if err != nil {
         return err
      }
   }
   return nil
}

func (f *Filter) String() string {
   var parts []string
   if f.Bandwidth >= 1 {
      parts = append(parts, fmt.Sprintf("b=%d", f.Bandwidth))
   }
   if f.Codecs != "" {
      parts = append(parts, "c="+f.Codecs)
   }
   if f.Height >= 1 {
      parts = append(parts, fmt.Sprintf("h=%d", f.Height))
   }
   if f.Id != "" {
      parts = append(parts, "i="+f.Id)
   }
   if f.Lang != "" {
      parts = append(parts, "l="+f.Lang)
   }
   if f.Role != "" {
      parts = append(parts, "r="+f.Role)
   }
   return strings.Join(parts, ",")
}

func (f *Filter) index(groups [][]*dash.Representation) int {
   minScore := math.MaxInt
   bestStream := -1

   for groupIndex, group := range groups {
      match, score := f.matchAndScore(group)
      if match == -1 {
         continue
      }
      if score < minScore {
         minScore = score
         bestStream = groupIndex
      }
   }
   return bestStream
}

func (f *Filter) matchAndScore(group []*dash.Representation) (int, int) {
   rep := group[0]

   if f.Codecs != "" {
      if !strings.HasPrefix(rep.GetCodecs(), f.Codecs) {
         return -1, 0
      }
   }
   if f.Height >= 1 {
      if rep.GetHeight() != f.Height {
         return -1, 0
      }
   }
   if f.Id != "" {
      if rep.ID != f.Id {
         return -1, 0
      }
   }
   if f.Lang != "" {
      if rep.Parent.Lang != f.Lang {
         return -1, 0
      }
   }
   if f.Role != "" {
      if rep.Parent.Role == nil {
         return -1, 0
      }
      if rep.Parent.Role.Value != f.Role {
         return -1, 0
      }
   }

   const penaltyFactor = 2
   score := rep.Bandwidth - f.Bandwidth
   if score < 0 {
      score = (f.Bandwidth - rep.Bandwidth) * penaltyFactor
   }
   return 0, score
}

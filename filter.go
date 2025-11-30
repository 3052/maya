package net

import (
   "41.neocities.org/dash"
   "41.neocities.org/sofia"
   "errors"
   "fmt"
   "io"
   "math"
   "net/http"
   "net/url"
   "slices"
   "strings"
)

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

   slices.SortFunc(groups, func(a, b []*dash.Representation) int {
      return a[0].Bandwidth - b[0].Bandwidth
   })

   for index, group := range groups {
      if index >= 1 {
         fmt.Println()
      }
      // Basic representation info
      fmt.Println(group[0])

      // --- Start of Bitrate Calculation Logic ---
      // 1. Pick the representation in the middle Period
      middleRep := group[len(group)/2]

      // 2. Calculate bitrate of the middle segment
      if bitrate, err := getMiddleBitrate(middleRep); err == nil {
         fmt.Printf("middle segment bitrate = %d\n", bitrate)
      }
      // --- End of Bitrate Calculation Logic ---
   }

   for _, target := range f.Values {
      index := target.index(groups)
      if index == -1 {
         continue
      }

      group := groups[index]
      if err := configVar.download(group); err != nil {
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

   for index, group := range groups {
      match, score := f.matchAndScore(group)
      if match == -1 {
         continue
      }
      if score < minScore {
         minScore = score
         bestStream = index
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

const FilterUsage = `b = bandwidth
c = codecs
h = height
i = id
l = lang
r = role`

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

// getMiddleBitrate calculates the bitrate of the middle segment for a specific Representation.
func getMiddleBitrate(rep *dash.Representation) (int, error) {
   baseURL, err := rep.ResolveBaseURL()
   if err != nil {
      return 0, err
   }

   // Strategy 1: SegmentBase (Single file with sidx)
   // We must download the sidx to find the segment size and duration.
   if rep.SegmentBase != nil {
      head := http.Header{}
      head.Set("Range", "bytes="+rep.SegmentBase.IndexRange)

      // reuse getSegment from config.go (same package)
      sidxData, err := getSegment(baseURL, head)
      if err != nil {
         return 0, err
      }

      parsed, err := sofia.Parse(sidxData)
      if err != nil {
         return 0, err
      }

      sidx, ok := sofia.FindSidx(parsed)
      if !ok {
         return 0, sofia.Missing("sidx")
      }

      if len(sidx.References) == 0 {
         return 0, errors.New("no references in sidx")
      }

      // Find Middle Segment
      midIdx := len(sidx.References) / 2
      ref := sidx.References[midIdx]

      sizeBits := uint64(ref.ReferencedSize) * 8
      // duration is in timescale units
      durationSec := float64(ref.SubsegmentDuration) / float64(sidx.Timescale)

      if durationSec <= 0 {
         return 0, errors.New("invalid duration")
      }

      return int(float64(sizeBits) / durationSec), nil
   }

   // Strategy 2: SegmentTemplate or SegmentList (Multiple files)
   // We resolve the URL list, pick the middle one, use HEAD for size,
   // and MPD info for duration.
   var urls []*url.URL
   var durationSec float64

   if tmpl := rep.GetSegmentTemplate(); tmpl != nil {
      u, err := tmpl.GetSegmentURLs(rep)
      if err != nil {
         return 0, err
      }
      urls = u

      // Calculate Duration for the middle segment
      midIdx := len(urls) / 2
      timescale := float64(tmpl.GetTimescale())

      if tmpl.SegmentTimeline != nil {
         // Find duration 'd' for the segment at midIdx
         currentIndex := 0
         found := false
         for _, s := range tmpl.SegmentTimeline.S {
            count := 1
            if s.R > 0 {
               count += s.R
            }
            // If midIdx falls within this S element
            if midIdx < currentIndex+count {
               durationSec = float64(s.D) / timescale
               found = true
               break
            }
            currentIndex += count
         }
         if !found {
            return 0, errors.New("could not find duration in timeline")
         }
      } else if tmpl.Duration > 0 {
         durationSec = float64(tmpl.Duration) / timescale
      } else {
         return 0, errors.New("unknown segment duration")
      }

   } else if list := rep.SegmentList; list != nil {
      for _, seg := range list.SegmentURLs {
         u, err := seg.ResolveMedia()
         if err != nil {
            return 0, err
         }
         urls = append(urls, u)
      }
      if list.Duration == 0 {
         return 0, errors.New("unknown segment duration")
      }
      durationSec = float64(list.Duration) / float64(list.GetTimescale())
   }

   if len(urls) == 0 {
      return 0, nil
   }

   // Fetch Size of Middle Segment
   midIdx := len(urls) / 2
   targetURL := urls[midIdx]

   resp, err := http.DefaultClient.Head(targetURL.String())
   if err != nil {
      return 0, err
   }
   defer resp.Body.Close()

   if resp.ContentLength <= 0 {
      return 0, errors.New("content length missing")
   }

   sizeBits := uint64(resp.ContentLength) * 8
   if durationSec <= 0 {
      return 0, errors.New("invalid duration")
   }

   return int(float64(sizeBits) / durationSec), nil
}

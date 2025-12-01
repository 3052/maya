package net

import (
   "41.neocities.org/dash"
   "41.neocities.org/sofia"
   "errors"
   "fmt"
   "io"
   "net/http"
   "net/url"
   "slices"
   "strings"
)

// Filter parses the MPD, prints available representations (calculating bitrates for the middle segment),
// and optionally downloads the representation matching the RepresentationId in Config.
func (c *Config) Filter(response *http.Response) error {
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

   // 1. Update Phase: Calculate bitrate for the middle representation of each group
   for _, group := range groups {
      middleRep := group[len(group)/2]
      if err := getMiddleBitrate(middleRep); err != nil {
         return err
      }
   }

   // 2. Print Phase: Display the representations
   for index, group := range groups {
      if index >= 1 {
         fmt.Println()
      }
      middleRep := group[len(group)/2]
      fmt.Println(middleRep)
   }

   if c.RepresentationId == "" {
      return nil
   }

   for _, group := range groups {
      // All representations in a group share the same ID
      if group[0].ID == c.RepresentationId {
         return c.download(group)
      }
   }

   return fmt.Errorf("representation '%s' not found", c.RepresentationId)
}

// getMiddleBitrate calculates the bitrate of the middle segment and updates the Representation.
func getMiddleBitrate(rep *dash.Representation) error {
   baseURL, err := rep.ResolveBaseURL()
   if err != nil {
      return err
   }

   // Strategy 1: SegmentBase (Single file with sidx)
   if rep.SegmentBase != nil {
      head := http.Header{}
      head.Set("Range", "bytes="+rep.SegmentBase.IndexRange)

      // reuse getSegment from config.go (same package)
      sidxData, err := getSegment(baseURL, head)
      if err != nil {
         return err
      }

      parsed, err := sofia.Parse(sidxData)
      if err != nil {
         return err
      }

      sidx, ok := sofia.FindSidx(parsed)
      if !ok {
         return sofia.Missing("sidx")
      }

      if len(sidx.References) == 0 {
         return errors.New("no references in sidx")
      }

      // Find Middle Segment
      midIdx := len(sidx.References) / 2
      ref := sidx.References[midIdx]

      sizeBits := uint64(ref.ReferencedSize) * 8
      // duration is in timescale units
      durationSec := float64(ref.SubsegmentDuration) / float64(sidx.Timescale)

      if durationSec <= 0 {
         return errors.New("invalid duration")
      }

      // Update Representation
      rep.Bandwidth = int(float64(sizeBits) / durationSec)
      return nil
   }

   // Strategy 2: SegmentTemplate or SegmentList (Multiple files)
   var urls []*url.URL
   var durationSec float64

   if tmpl := rep.GetSegmentTemplate(); tmpl != nil {
      u, err := tmpl.GetSegmentURLs(rep)
      if err != nil {
         return err
      }
      urls = u

      // Calculate Duration for the middle segment
      midIdx := len(urls) / 2
      timescale := float64(tmpl.GetTimescale())

      if tmpl.SegmentTimeline != nil {
         currentIndex := 0
         found := false
         for _, s := range tmpl.SegmentTimeline.S {
            count := 1
            if s.R > 0 {
               count += s.R
            }
            if midIdx < currentIndex+count {
               durationSec = float64(s.D) / timescale
               found = true
               break
            }
            currentIndex += count
         }
         if !found {
            return errors.New("could not find duration in timeline")
         }
      } else if tmpl.Duration > 0 {
         durationSec = float64(tmpl.Duration) / timescale
      } else {
         return errors.New("unknown segment duration")
      }

   } else if list := rep.SegmentList; list != nil {
      for _, seg := range list.SegmentURLs {
         u, err := seg.ResolveMedia()
         if err != nil {
            return err
         }
         urls = append(urls, u)
      }
      if list.Duration == 0 {
         return errors.New("unknown segment duration")
      }
      durationSec = float64(list.Duration) / float64(list.GetTimescale())
   }

   if len(urls) == 0 {
      rep.Bandwidth = 0
      return nil
   }

   // Fetch Size of Middle Segment
   midIdx := len(urls) / 2
   targetURL := urls[midIdx]

   resp, err := http.DefaultClient.Head(targetURL.String())
   if err != nil {
      return err
   }
   defer resp.Body.Close()

   if resp.ContentLength <= 0 {
      return errors.New("content length missing")
   }

   sizeBits := uint64(resp.ContentLength) * 8
   if durationSec <= 0 {
      return errors.New("invalid duration")
   }

   // Update Representation
   rep.Bandwidth = int(float64(sizeBits) / durationSec)
   return nil
}

package net

import (
   "41.neocities.org/dash"
   "41.neocities.org/sofia"
   "errors"
   "fmt"
   "log"
   "net/http"
   "net/url"
   "slices"
)

// Download parses the MPD from a byte slice and downloads the specified representation.
func (c *Config) Download(mpdBody, mpdUrl, representationId string) error {
   mpd, err := dash.Parse([]byte(mpdBody))
   if err != nil {
      return err
   }
   mpd.MPDURL, err = url.Parse(mpdUrl)
   if err != nil {
      return err
   }

   for _, group := range mpd.GetRepresentations() {
      // All representations in a group share the same ID.
      // We check the first one, ensuring the group is not empty.
      if len(group) > 0 && group[0].ID == representationId {
         return c.downloadGroup(group)
      }
   }

   return fmt.Errorf("representation '%s' not found", representationId)
}

// PrintRepresentations parses the MPD, calculates the true bitrate for the middle
// representation of each group, and prints them in sorted order.
func (c *Config) Representations(mpdBody, mpdUrl string) error {
   mpd, err := dash.Parse([]byte(mpdBody))
   if err != nil {
      return err
   }
   mpd.MPDURL, err = url.Parse(mpdUrl)
   if err != nil {
      return err
   }

   // 1. Build a slice of middle representations, updating their bitrates as we go.
   var middleReps []*dash.Representation
   for _, group := range mpd.GetRepresentations() {
      if len(group) > 0 {
         middleRep := group[len(group)/2]
         if err := getMiddleBitrate(middleRep); err != nil {
            return err
         }
         middleReps = append(middleReps, middleRep)
      }
   }

   // 2. Sort Phase: Sort the representations based on their new, accurate bitrates.
   slices.SortFunc(middleReps, func(a, b *dash.Representation) int {
      return a.Bandwidth - b.Bandwidth
   })

   // 3. Print Phase: Display the sorted representations.
   for index, rep := range middleReps {
      if index >= 1 {
         fmt.Println()
      }
      fmt.Println(rep)
   }
   return nil
}

// getMiddleBitrate calculates the bitrate of the middle segment and updates the Representation.
func getMiddleBitrate(rep *dash.Representation) error {
   baseURL, err := rep.ResolveBaseURL()
   if err != nil {
      return err
   }
   log.Println("update", rep.ID)

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

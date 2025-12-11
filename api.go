package net

import (
   "41.neocities.org/dash"
   "fmt"
   "net/url"
   "slices"
)

// Representations parses the MPD, calculates the true bitrate for the middle
// representation of each group, and prints them in sorted order.
func Representations(mpd *url.URL, mpdBody []byte) error {
   manifest, err := dash.Parse(mpdBody)
   if err != nil {
      return err
   }
   manifest.MPDURL = mpd

   // 1. Build a slice of middle representations, updating their bitrates as we go.
   var middleReps []*dash.Representation
   for _, group := range manifest.GetRepresentations() {
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

// Download parses the MPD from a byte slice and downloads the specified representation.
func (c *Config) Download(mpd *url.URL, mpdBody []byte, representationId string) error {
   manifest, err := dash.Parse(mpdBody)
   if err != nil {
      return err
   }
   manifest.MPDURL = mpd

   for _, group := range manifest.GetRepresentations() {
      // All representations in a group share the same ID.
      // We check the first one, ensuring the group is not empty.
      if len(group) > 0 && group[0].ID == representationId {
         return c.downloadGroup(group)
      }
   }

   return fmt.Errorf("representation '%s' not found", representationId)
}

// Config holds downloader configuration
type Config struct {
   Send             func([]byte) ([]byte, error)
   Threads          int
   CertificateChain string
   EncryptSignKey   string
   ClientId         string
   PrivateKey       string
   DecryptionKey    string
}

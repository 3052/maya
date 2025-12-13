package net

import (
   "41.neocities.org/dash"
   "fmt"
   "net/url"
   "slices"
   "strconv"
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
      middleRep := group[len(group)/2]
      if err := getMiddleBitrate(middleRep); err != nil {
         return err
      }
      middleReps = append(middleReps, middleRep)
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

// Download retrieves the specific group of Representations from the MPD
// matching the provided hex-encoded hash key.
func (c *Config) Download(mpd *url.URL, mpdBody []byte, hexKey string) error {
   manifest, err := dash.Parse(mpdBody)
   if err != nil {
      return err
   }
   manifest.MPDURL = mpd
   // Parse the safe hex string back into the uint32 hash
   hash, err := strconv.ParseUint(hexKey, 16, 32)
   if err != nil {
      return fmt.Errorf("invalid hash format: %w", err)
   }
   // Use GetRepresentations as the Source of Truth for grouping logic
   allGroups := manifest.GetRepresentations()
   group, ok := allGroups[uint32(hash)]
   if !ok {
      return fmt.Errorf("representation group %s not found", hexKey)
   }
   return c.downloadGroup(group)
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

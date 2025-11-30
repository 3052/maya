package net

import (
   "41.neocities.org/dash"
   "41.neocities.org/drm/widevine"
   "41.neocities.org/sofia"
   "crypto/aes"
   "log"
   "os"
   "strings"
)

func (m *MediaFile) initializeWriter(file *os.File, initData []byte) (*sofia.Unfragmenter, error) {
   var unfrag sofia.Unfragmenter
   unfrag.Writer = file
   if len(initData) > 0 {
      // Initialize parses the init segment and sets unfrag.Moov
      if err := unfrag.Initialize(initData); err != nil {
         return nil, err
      }
      // Read/Update Moov (extract content ID/timescale, remove PSSH/EDTS)
      if err := m.configureMoov(unfrag.Moov); err != nil {
         return nil, err
      }
   }
   return &unfrag, nil
}

func (m *MediaFile) processAndWriteSegments(
   doneChan chan<- error,
   results <-chan result,
   totalSegments int,
   key []byte,
   unfrag *sofia.Unfragmenter,
) {
   // Setup Decryption Block once if key is present
   if len(key) > 0 {
      block, err := aes.NewCipher(key)
      if err != nil {
         doneChan <- err
         return
      }
      // Decrypt samples in place using the block
      unfrag.OnSample = func(sample []byte, info *sofia.SampleEncryptionInfo) {
         sofia.DecryptSample(sample, info, block)
      }
   }

   // Setup Progress Tracking
   prog := newProgress(totalSegments)

   pending := make(map[int][]byte)
   nextIndex := 0
   for i := 0; i < totalSegments; i++ {
      res := <-results
      if res.err != nil {
         doneChan <- res.err
         return
      }
      pending[res.index] = res.data

      // Write all available sequential segments
      for {
         data, ok := pending[nextIndex]
         if !ok {
            break
         }

         // AddSegment decrypts samples, writes mdat payload to file, and triggers OnSampleInfo
         if err := unfrag.AddSegment(data); err != nil {
            doneChan <- err
            return
         }

         // Update progress once per segment
         prog.update()

         delete(pending, nextIndex)
         nextIndex++
      }
   }

   // Finish writes the final moov box and updates mdat size
   if err := unfrag.Finish(); err != nil {
      doneChan <- err
      return
   }
   doneChan <- nil
}

type MediaFile struct {
   timescale  uint32
   key_id     []byte
   content_id []byte
}

func (m *MediaFile) configureProtection(rep *dash.Representation) error {
   for _, protect := range rep.GetContentProtection() {
      switch strings.ToLower(protect.SchemeIdUri) {
      case protectionURN:
         // 1. get `default_KID` from MPD
         // https://ctv.ca MPD is missing PSSH
         data, err := protect.GetDefaultKID()
         if err != nil {
            return err
         }
         if data != nil {
            m.key_id = data
            log.Printf("key ID %x", m.key_id)
         }
      case widevineURN:
         // 2. check if MPD has PSSH, check if PSSH has content ID
         // https://hulu.com poisons the PSSH so we only want content ID
         data, err := protect.GetPSSH()
         if err != nil {
            return err
         }
         if data != nil {
            var pssh_box sofia.PsshBox
            err = pssh_box.Parse(data)
            if err != nil {
               return err
            }
            var pssh_data widevine.PsshData
            err = pssh_data.Unmarshal(pssh_box.Data)
            if err != nil {
               return err
            }
            if pssh_data.ContentID != nil {
               m.content_id = pssh_data.ContentID
               log.Printf("DASH content ID %x", m.content_id)
            }
         }
      }
   }
   return nil
}

func (m *MediaFile) configureMoov(moov *sofia.MoovBox) error {
   // Handle Widevine PSSH
   if wvBox, ok := moov.FindPssh(widevineID); ok {
      var pssh_data widevine.PsshData
      if err := pssh_data.Unmarshal(wvBox.Data); err != nil {
         return err
      }
      if pssh_data.ContentID != nil {
         m.content_id = pssh_data.ContentID
         log.Printf("MP4 content ID %x", m.content_id)
      }
   }
   moov.RemovePssh()

   // Unfragmenter.Initialize guarantees Trak exists
   trak, _ := moov.Trak()
   trak.RemoveEdts()

   mdia, ok := trak.Mdia()
   if !ok {
      return sofia.Missing("mdia")
   }
   mdhd, ok := mdia.Mdhd()
   if !ok {
      return sofia.Missing("mdhd")
   }
   m.timescale = mdhd.Timescale
   return nil
}

const (
   protectionURN = "urn:mpeg:dash:mp4protection:2011"
   widevineURN   = "urn:uuid:edef8ba9-79d6-4ace-a3c8-27dcd51d21ed"
)

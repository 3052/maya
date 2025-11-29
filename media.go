package net

import (
   "41.neocities.org/dash"
   "41.neocities.org/drm/widevine"
   "41.neocities.org/sofia"
   "bytes"
   "crypto/aes"
   "log"
   "os"
   "strings"
)

func (m *MediaFile) processAndWriteSegments(
   doneChan chan<- error,
   results <-chan result,
   totalSegments int,
   key []byte,
   file *os.File,
   initData []byte,
) {
   var unfrag sofia.Unfragmenter
   unfrag.Writer = file

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
   unfrag.OnSampleInfo = func(sample *sofia.UnfragSample) {
      prog.update(uint64(sample.Size), uint64(sample.Duration), m.timescale)
   }

   // Handle Initialization
   if len(initData) > 0 {
      // The unfragmenter builds the file structure (mdat header, then payload, then moov).
      // However, it does not write the `ftyp` box. We must extract and write `ftyp` manually
      // from the initData before initializing the unfragmenter (which writes the mdat header).
      boxes, err := sofia.Parse(initData)
      if err != nil {
         doneChan <- err
         return
      }
      for _, box := range boxes {
         if string(box.Raw[4:8]) == "ftyp" {
            if _, err := file.Write(box.Raw); err != nil {
               doneChan <- err
               return
            }
            break
         }
      }

      // Initialize unfragmenter (writes mdat header, parses moov from initData)
      if err := unfrag.Initialize(initData); err != nil {
         doneChan <- err
         return
      }
   }

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

         // AddSegment decrypts samples and writes mdat payload to file
         if err := unfrag.AddSegment(data); err != nil {
            doneChan <- err
            return
         }

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
   timescale  uint64
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

func (m *MediaFile) processInit(data []byte) ([]byte, error) {
   parsedInit, err := sofia.Parse(data)
   if err != nil {
      return nil, err
   }

   // 1 pssh
   moov, ok := sofia.FindMoov(parsedInit)
   if !ok {
      return nil, sofia.Missing("moov")
   }

   if wvBox, ok := moov.FindPssh(widevineID); ok {
      var pssh_data widevine.PsshData
      err = pssh_data.Unmarshal(wvBox.Data)
      if err != nil {
         return nil, err
      }
      if pssh_data.ContentID != nil {
         m.content_id = pssh_data.ContentID
         log.Printf("MP4 content ID %x", m.content_id)
      }
   }
   moov.RemovePssh()

   // 2 edts
   trak, ok := moov.Trak()
   if !ok {
      return nil, sofia.Missing("trak")
   }
   trak.RemoveEdts()

   // 3 mdhd
   mdia, ok := trak.Mdia()
   if !ok {
      return nil, sofia.Missing("mdia")
   }

   mdhd, ok := mdia.Mdhd()
   if !ok {
      return nil, sofia.Missing("mdhd")
   }
   m.timescale = uint64(mdhd.Timescale)

   // 4 stsd
   minf, ok := mdia.Minf()
   if !ok {
      return nil, sofia.Missing("minf")
   }

   stbl, ok := minf.Stbl()
   if !ok {
      return nil, sofia.Missing("stbl")
   }

   stsd, ok := stbl.Stsd()
   if !ok {
      return nil, sofia.Missing("stsd")
   }

   err = stsd.UnprotectAll()
   if err != nil {
      return nil, err
   }

   var buf bytes.Buffer
   for _, box := range parsedInit {
      buf.Write(box.Encode())
   }
   return buf.Bytes(), nil
}

const (
   protectionURN = "urn:mpeg:dash:mp4protection:2011"
   widevineURN   = "urn:uuid:edef8ba9-79d6-4ace-a3c8-27dcd51d21ed"
)

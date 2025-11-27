package net

import (
   "41.neocities.org/dash"
   "41.neocities.org/drm/widevine"
   "41.neocities.org/sofia"
   "bytes"
   "log"
   "os"
   "strings"
)

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
   moov, ok := sofia.FindMoov(parsedInit)
   if !ok {
      return nil, sofia.Missing("moov")
   }
   // 3. check if initialization has PSSH, check if PSSH has content ID
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
   trak, ok := moov.Trak()
   if !ok {
      return nil, sofia.Missing("trak")
   }
   trak.ReplaceEdts()
   mdia, ok := trak.Mdia()
   if !ok {
      return nil, sofia.Missing("mdia")
   }
   mdhd, ok := mdia.Mdhd()
   if !ok {
      return nil, sofia.Missing("mdhd")
   }
   m.timescale = uint64(mdhd.Timescale)
   err = moov.Sanitize()
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

func (m *MediaFile) processSegment(data, key []byte, p *progress, sidx *sofia.SidxBox) ([]byte, error) {
   parsed, err := sofia.Parse(data)
   if err != nil {
      return nil, err
   }

   // Calculate stats for progress and sidx
   var sampleSize, duration uint64
   for _, moof := range sofia.AllMoof(parsed) {
      traf, ok := moof.Traf()
      if !ok {
         return nil, ErrMissingTraf
      }
      bytes, dur, err := traf.Totals()
      if err != nil {
         return nil, err
      }
      sampleSize += bytes
      duration += dur
   }

   p.update(sampleSize, duration, m.timescale)

   // Handle decryption
   if key != nil {
      if err := sofia.Decrypt(parsed, key); err != nil {
         return nil, err
      }
      // Re-encode
      var buf bytes.Buffer
      buf.Grow(len(data))
      for _, box := range parsed {
         buf.Write(box.Encode())
      }
      data = buf.Bytes()
   }

   // Update sidx
   if sidx != nil {
      if err := sidx.AddReference(uint32(len(data)), uint32(duration)); err != nil {
         return nil, err
      }
   }

   return data, nil
}

func (m *MediaFile) processAndWriteSegments(
   doneChan chan<- error,
   results <-chan result,
   totalSegments int,
   key []byte,
   file *os.File,
   sidx *sofia.SidxBox,
) {
   pending := make(map[int][]byte)
   nextIndex := 0
   prog := newProgress(totalSegments)

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
         processedData, err := m.processSegment(data, key, prog, sidx)
         if err != nil {
            doneChan <- err
            return
         }
         if _, err = file.Write(processedData); err != nil {
            doneChan <- err
            return
         }
         delete(pending, nextIndex)
         nextIndex++
      }
   }
   doneChan <- nil
}

type MediaFile struct {
   timescale  uint64
   key_id     []byte
   content_id []byte
}

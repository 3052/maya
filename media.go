package net

import (
   "41.neocities.org/dash"
   "41.neocities.org/drm/widevine"
   "41.neocities.org/sofia"
   "bytes"
   "log"
   "os"
)

func (m *MediaFile) configureProtection(rep *dash.Representation) error {
   for protect := range rep.GetContentProtection() {
      switch protect.SchemeIdUri {
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
               log.Println("DASH content ID", string(m.content_id))
            }
         }
      }
   }
   return nil
}

const protectionURN = "urn:mpeg:dash:mp4protection:2011"

const widevineURN = "urn:uuid:edef8ba9-79d6-4ace-a3c8-27dcd51d21ed"

func (m *MediaFile) processSegment(data, key []byte) ([]byte, error) {
   if key == nil {
      return data, nil
   }
   parsed, err := sofia.Parse(data)
   if err != nil {
      return nil, err
   }

   for _, moof := range sofia.AllMoof(parsed) {
      traf, ok := moof.Traf()
      if !ok {
         return nil, ErrMissingTraf
      }
      bytes, dur, err := traf.Totals()
      if err != nil {
         return nil, err
      }
      m.size += bytes
      m.duration += dur
   }

   if err := sofia.Decrypt(parsed, key); err != nil {
      return nil, err
   }

   var buf bytes.Buffer
   for _, box := range parsed {
      buf.Write(box.Encode())
   }
   return buf.Bytes(), nil
}

func (m *MediaFile) processAndWriteSegments(
   doneChan chan<- error,
   results <-chan result,
   totalSegments int,
   key []byte,
   file *os.File,
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

         processedData, err := m.processSegment(data, key)
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
         prog.update(m.size, m.duration, m.timescale)
      }
   }
   doneChan <- nil
}

type MediaFile struct {
   timescale  uint64
   size       uint64
   duration   uint64
   key_id     []byte
   content_id []byte
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
         log.Println("MP4 content ID", string(m.content_id))
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

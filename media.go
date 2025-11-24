package net

import (
   "41.neocities.org/dash"
   "41.neocities.org/sofia"
   "bytes"
   "encoding/base64"
   "log"
   "os"
)

// 1. get `default_KID` from MPD
// https://ctv.ca MPD is missing PSSH
// 
// 2. check if MPD has PSSH, check if PSSH has content ID
// https://hulu.com poisons the PSSH so we only want content ID
// 
// 3. check if initialization has PSSH, check if PSSH has content ID

func (m *MediaFile) processInit(data []byte) ([]byte, error) {
   parsedInit, err := sofia.Parse(data)
   if err != nil {
      return nil, err
   }
   moov, ok := sofia.FindMoov(parsedInit)
   if !ok {
      return nil, sofia.Missing("moov")
   }

   if m.pssh == nil {
      if wvBox, ok := moov.FindPssh(widevineID); ok {
         m.pssh = wvBox.Data
         log.Println("MP4 PSSH", base64.StdEncoding.EncodeToString(m.pssh))
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
   
   // FIXME NEED KEY ID

   if err := moov.Sanitize(); err != nil {
      return nil, err
   }

   var buf bytes.Buffer
   for _, box := range parsedInit {
      buf.Write(box.Encode())
   }
   return buf.Bytes(), nil
}

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
   fileVar *os.File,
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
         if _, err = fileVar.Write(processedData); err != nil {
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
   keyID     []byte
   pssh      []byte
   timescale uint64
   size      uint64
   duration  uint64
}

func (m *MediaFile) configureProtection(rep *dash.Representation) error {
   for protect := range rep.GetContentProtection() {
      if protect.SchemeIdUri == widevineURN {
         if protect.Pssh != "" {
            data, err := base64.StdEncoding.DecodeString(protect.Pssh)
            if err != nil {
               return err
            }
            var box sofia.PsshBox
            if err := box.Parse(data); err != nil {
               return err
            }
            m.pssh = box.Data
            log.Println("MPD PSSH", base64.StdEncoding.EncodeToString(m.pssh))
            break
         }
      }
   }
   return nil
}

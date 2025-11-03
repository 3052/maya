package net

import (
   "41.neocities.org/sofia"
   "bytes"
   "log"
)

// segment can be VTT or anything
func (m *media_file) write_segment(data, key []byte) ([]byte, error) {
   if key == nil {
      return data, nil
   }
   parsedSegment, err := sofia.Parse(data)
   if err != nil {
      return nil, err
   }
   if m.duration/m.timescale < 10*60 {
      for _, moof := range sofia.AllMoof(parsedSegment) {
         traf, ok := moof.Traf()
         if !ok {
            return nil, sofia.Missing("traf")
         }
         total_bytes, total_duration, err := traf.Totals()
         if err != nil {
            return nil, err
         }
         m.size += total_bytes
         m.duration += total_duration
      }
      // Bandwidth in bps = (TotalBytes * 8 bits/byte) /
      // (TotalDuration / Timescale in seconds)
      // Simplified: (TotalBytes * 8 * Timescale) / TotalDuration
      log.Println("bandwidth", m.size * 8 * m.timescale / m.duration)
   }
   err = sofia.Decrypt(parsedSegment, key)
   if err != nil {
      return nil, err
   }
   var finalMP4Data bytes.Buffer
   for _, box := range parsedSegment {
      finalMP4Data.Write(box.Encode())
   }
   return finalMP4Data.Bytes(), nil
}

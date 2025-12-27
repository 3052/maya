package maya

import (
   "41.neocities.org/sofia"
   "bytes"
)

// detectionResult holds the outcome of content analysis.
type detectionResult struct {
   Extension string
   IsFMP4    bool
}

var (
   // Signature for VTT file type.
   vttSignature = []byte("WEBVTT")
)

// detectContentType inspects the first chunk of data from a stream to determine
// its file type and whether it requires remuxing. It returns a zero-value
// result if the type is unknown.
func detectContentType(data []byte) detectionResult {
   // Check for MP4 by looking for a 'moov' box.
   if boxes, err := sofia.Parse(data); err == nil {
      if moov, ok := sofia.FindMoov(boxes); ok {
         if moov.IsAudio() {
            return detectionResult{Extension: ".m4a", IsFMP4: true}
         }
         return detectionResult{Extension: ".mp4", IsFMP4: true}
      }
   }

   // Check for WebVTT.
   if bytes.HasPrefix(data, vttSignature) {
      return detectionResult{Extension: ".vtt"}
   }

   // Return zero-value if no known type is detected.
   return detectionResult{}
}

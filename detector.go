package maya

import (
   "41.neocities.org/luna/dash"
   "41.neocities.org/luna/hls"
   "fmt"
   "net/url"
   "strconv"
)

// TypeInfo holds the determined properties of a media stream.
type TypeInfo struct {
   Extension string
   IsFMP4    bool
}

// DetectDashType determines the file extension and container type from a DASH Representation's metadata.
func DetectDashType(rep *dash.Representation) (*TypeInfo, error) {
   switch rep.GetMimeType() {
   case "video/mp4":
      return &TypeInfo{Extension: ".mp4", IsFMP4: true}, nil
   case "audio/mp4":
      return &TypeInfo{Extension: ".m4a", IsFMP4: true}, nil
   case "text/vtt":
      return &TypeInfo{Extension: ".vtt", IsFMP4: false}, nil
   default:
      return nil, fmt.Errorf("unsupported mime type for stream %s: %s", rep.Id, rep.GetMimeType())
   }
}

// DetectHlsType finds the correct stream in an HLS playlist by its ID and determines its type.
// It returns the type info, the target URI for the media playlist, and any error.
func DetectHlsType(playlist *hls.MasterPlaylist, streamId string) (*TypeInfo, *url.URL, error) {
   keyInt, err := strconv.Atoi(streamId)
   if err != nil {
      return nil, nil, fmt.Errorf("invalid HLS StreamId, must be an integer: %q", streamId)
   }

   // Check variant streams (multiplexed video/audio)
   for _, variant := range playlist.StreamInfs {
      if variant.ID == keyInt {
         // Variant streams are treated as primary MP4 content.
         info := &TypeInfo{Extension: ".mp4", IsFMP4: true}
         return info, variant.URI, nil
      }
   }

   // Check media renditions (audio-only, subtitles)
   for _, rendition := range playlist.Medias {
      if rendition.ID == keyInt {
         var info *TypeInfo
         switch rendition.Type {
         case "AUDIO":
            info = &TypeInfo{Extension: ".m4a", IsFMP4: true}
         case "SUBTITLES":
            info = &TypeInfo{Extension: ".vtt", IsFMP4: false}
         default:
            return nil, nil, fmt.Errorf("unsupported HLS media type: %s", rendition.Type)
         }
         return info, rendition.URI, nil
      }
   }

   return nil, nil, fmt.Errorf("stream with ID not found: %d", keyInt)
}

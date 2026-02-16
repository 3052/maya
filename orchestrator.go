package maya

import (
   "41.neocities.org/sofia"
   "bytes"
   "encoding/binary"
   "encoding/hex"
   "net/http"
   "net/url"
   "os"
   "strings"
)

// mediaRequest represents a single segment to be downloaded.
type mediaRequest struct {
   url    *url.URL
   header http.Header
}

// downloadJob holds all the extracted, manifest-agnostic information needed to run a download.
type downloadJob struct {
   streamId           string
   typeInfo           *typeInfo
   allRequests        []mediaRequest
   initSegmentData    []byte
   manifestProtection *protectionInfo
   threads            int
   fetchKey           keyFetcher
}

// orchestrateDownload contains the shared, high-level logic for executing any download job.
func orchestrateDownload(job *downloadJob) error {
   var name strings.Builder
   name.WriteString(strings.ReplaceAll(job.streamId, "/", "_"))
   name.WriteString(job.typeInfo.Extension)
   file, err := os.Create(name.String())
   if err != nil {
      return err
   }
   defer file.Close()

   if !job.typeInfo.IsFMP4 {
      // Non-FMP4 streams (e.g., VTT): download all segments and concatenate them directly.
      return executeDownload(job.allRequests, nil, nil, file, job.threads)
   }

   // FMP4 streams: require an initialization segment and a remuxer.
   remux, initProtection, err := initializeRemuxer(job.initSegmentData, file)
   if err != nil {
      return err
   }

   var key []byte
   if job.fetchKey != nil {
      key, err = getKeyForStream(job.fetchKey, job.manifestProtection, initProtection)
      if err != nil {
         return err
      }
   }
   return executeDownload(job.allRequests, key, remux, file, job.threads)
}

// findOuterBox locates the raw byte slice of a top-level box in a stream.
func findOuterBox(data []byte, outerBoxType [4]byte) ([]byte, bool) {
   offset := 0
   for offset < len(data) {
      if offset+8 > len(data) {
         return nil, false
      }
      size := binary.BigEndian.Uint32(data[offset:])
      if bytes.Equal(data[offset+4:offset+8], outerBoxType[:]) {
         if size == 0 {
            return data[offset:], true
         }
         if offset+int(size) > len(data) {
            return nil, false // Invalid size
         }
         return data[offset : offset+int(size)], true
      }
      if size == 0 {
         return nil, false
      }
      if size < 8 {
         return nil, false // Invalid size
      }
      offset += int(size)
   }
   return nil, false
}

// findInnerPssh locates the raw byte slice for a PSSH box inside a container box's payload.
func findInnerPssh(containerPayload []byte, systemID []byte) []byte {
   offset := 0
   for offset < len(containerPayload) {
      if offset+8 > len(containerPayload) {
         return nil
      }
      size := binary.BigEndian.Uint32(containerPayload[offset:])
      boxType := containerPayload[offset+4 : offset+8]

      if string(boxType) == "pssh" {
         if offset+28 <= len(containerPayload) {
            psshSysID := containerPayload[offset+12 : offset+28]
            if bytes.Equal(psshSysID, systemID) {
               if size == 0 {
                  size = uint32(len(containerPayload) - offset)
               }
               if offset+int(size) > len(containerPayload) {
                  return nil // Invalid size
               }
               return containerPayload[offset : offset+int(size)]
            }
         }
      }
      if size == 0 || size < 8 {
         return nil
      }
      offset += int(size)
   }
   return nil
}

func initializeRemuxer(firstData []byte, file *os.File) (*sofia.Remuxer, *protectionInfo, error) {
   var remux sofia.Remuxer
   remux.Writer = file
   if len(firstData) > 0 {
      if err := remux.Initialize(firstData); err != nil {
         return nil, nil, err
      }
   }

   var initProtection *protectionInfo
   if remux.Moov != nil {
      initProtection = &protectionInfo{}
      wvIdBytes, err := hex.DecodeString(widevineSystemId)
      if err != nil {
         panic("failed to decode hardcoded widevine system id")
      }

      // 1. Get raw PSSH bytes for Content ID lookup
      if moovData, ok := findOuterBox(firstData, [4]byte{'m', 'o', 'o', 'v'}); ok {
         moovPayload := moovData[8:] // Search within the moov payload
         if rawPssh := findInnerPssh(moovPayload, wvIdBytes); rawPssh != nil {
            initProtection.Pssh = rawPssh
         }
      }

      // 2. Get key ID ONLY from MP4 'tenc' box.
      if len(remux.Moov.Trak) > 0 {
         trak := remux.Moov.Trak[0]
         if trak.Mdia != nil && trak.Mdia.Minf != nil && trak.Mdia.Minf.Stbl != nil && trak.Mdia.Minf.Stbl.Stsd != nil {
            for _, enc := range trak.Mdia.Minf.Stbl.Stsd.EncChildren {
               if enc.Sinf != nil && enc.Sinf.Tenc != nil {
                  var zeroKid [16]byte
                  if !bytes.Equal(enc.Sinf.Tenc.DefaultKID[:], zeroKid[:]) {
                     initProtection.KeyId = enc.Sinf.Tenc.DefaultKID[:]
                     break
                  }
               }
            }
         }
      }
      remux.Moov.RemovePssh()
   }
   return &remux, initProtection, nil
}

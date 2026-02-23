package maya

import (
   "41.neocities.org/drm/widevine"
   "41.neocities.org/sofia"
   "bytes"
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
   outputFileNameBase string // RENAMED from streamId
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
   // REMOVED strings.ReplaceAll call.
   name.WriteString(job.outputFileNameBase)
   name.WriteString(job.typeInfo.Extension)

   // CHANGED: Use shared createFile to handle directories
   file, err := createFile(name.String())
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

      // 1. Get Content ID from the PSSH box in the init segment.
      if pssh, ok := remux.Moov.FindPssh(wvIdBytes); ok {
         var wvData widevine.PsshData
         if err := wvData.Unmarshal(pssh.Data); err == nil {
            initProtection.ContentId = wvData.ContentId
         }
      }
      // 2. Get key ID ONLY from the 'tenc' box.
      if len(remux.Moov.Trak) > 0 {
         trak := remux.Moov.Trak[0]
         if trak.Mdia != nil {
            if trak.Mdia.Minf != nil {
               if trak.Mdia.Minf.Stbl != nil {
                  if trak.Mdia.Minf.Stbl.Stsd != nil {
                     for _, enc := range trak.Mdia.Minf.Stbl.Stsd.EncChildren {
                        if enc.Sinf != nil && enc.Sinf.Schi != nil && enc.Sinf.Schi.Tenc != nil {
                           var zeroKid [16]byte
                           if !bytes.Equal(enc.Sinf.Schi.Tenc.DefaultKID[:], zeroKid[:]) {
                              initProtection.KeyId = enc.Sinf.Schi.Tenc.DefaultKID[:]
                              break
                           }
                        }
                     }
                  }
               }
            }
         }
      }
      remux.Moov.RemovePssh()
   }
   return &remux, initProtection, nil
}

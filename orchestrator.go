// orchestrator.go
package maya

import (
   "41.neocities.org/diana/playReady"
   "41.neocities.org/diana/widevine"
   "41.neocities.org/sofia"
   "bytes"
   "encoding/hex"
   "fmt"
   "log"
   "net/http"
   "net/url"
   "os"
   "path/filepath"
   "strings"
)

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
      prIdBytes, err := hex.DecodeString(playReadySystemId)
      if err != nil {
         panic("failed to decode hardcoded playready system id")
      }

      // 1. Get Content ID from the PSSH box in the init segment.
      if pssh, ok := remux.Moov.FindPssh(wvIdBytes); ok {
         wv_data, err := widevine.DecodePsshData(pssh.Data)
         if err == nil {
            initProtection.ContentId = wv_data.ContentId
         }
      }
      if initProtection.ContentId == nil {
         if pssh, ok := remux.Moov.FindPssh(prIdBytes); ok {
            wrm, err := playReady.ParsePro(pssh.Data)
            if err != nil {
               return nil, nil, fmt.Errorf("failed to parse PlayReady PRO: %w", err)
            }
            if wrm.Data.CustomAttributes != nil {
               initProtection.ContentId = []byte(wrm.Data.CustomAttributes.ContentId)
            }
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

// segment represents a single chunk to be downloaded.
// This is used for both DASH and HLS, mapping tasks to workers.
type segment struct {
   url      *url.URL
   header   http.Header
   duration float64 // Used mostly by DASH for bitrate calculations
   sizeBits uint64  // Used mostly by DASH for bitrate calculations
}

// downloadJob holds all the extracted, manifest-agnostic information needed to run a download.
type downloadJob struct {
   outputFileNameBase string // RENAMED from streamId
   typeInfo           *typeInfo
   allRequests        []segment
   initSegmentData    []byte
   manifestProtection *protectionInfo
   threads            int
   fetchKey           keyFetcher
}

// orchestrateDownload contains the shared, high-level logic for executing any download job.
func orchestrateDownload(job *downloadJob) error {
   var name strings.Builder
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

func createFile(name string) (*os.File, error) {
   err := os.MkdirAll(filepath.Dir(name), os.ModePerm)
   if err != nil {
      return nil, err
   }
   log.Println("Creating file:", name)
   return os.Create(name)
}

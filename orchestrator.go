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
   "net/url"
   "os"
   "path/filepath"
   "strings"
)

func createFile(name string) (*os.File, error) {
   err := os.MkdirAll(filepath.Dir(name), os.ModePerm)
   if err != nil {
      return nil, err
   }
   log.Println("create:", name)
   return os.Create(name)
}

// orchestrateDownload contains the shared, high-level logic for executing any download.
func orchestrateDownload(job *downloadJob) error {
   var name strings.Builder
   name.WriteString(job.outputFileNameBase)
   name.WriteString(job.info.Extension)

   // Phase 1: Sample bitrate to determine if the stream meets the minimum.
   // Only applies to fMP4 streams with a minimum bitrate specified.
   // No file is created during this phase — we may abort entirely.
   var cached map[int][]byte
   if job.info.IsFmp4 && job.minBitrate > 0 {
      var err error
      cached, err = sampleBitrate(job)
      if err != nil {
         return err
      }
   }

   // Phase 2: Create the file and download all segments.
   // Cached segments from Phase 1 are written from memory;
   // remaining segments are downloaded via the worker pool.
   file, err := createFile(name.String())
   if err != nil {
      return err
   }
   defer file.Close()

   if !job.info.IsFmp4 {
      return executeDownload(job.allRequests, nil, nil, file, job.threads, cached)
   }

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
   return executeDownload(job.allRequests, key, remux, file, job.threads, cached)
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
      prIdBytes, err := hex.DecodeString(playReadySystemId)
      if err != nil {
         panic("failed to decode hardcoded playready system id")
      }

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

// downloadJob holds all the extracted, manifest-agnostic information needed to run a download.
type downloadJob struct {
   outputFileNameBase string
   info               *typeInfo
   allRequests        []segment
   initSegmentData    []byte
   manifestProtection *protectionInfo
   threads            int
   fetchKey           keyFetcher
   minBitrate         int
}

// segment represents a single chunk to be downloaded.
type segment struct {
   url      *url.URL
   headers  map[string]string
   duration float64
   sizeBits uint64
}

// typeInfo holds the determined properties of a media stream
type typeInfo struct {
   Extension string
   IsFmp4    bool
}

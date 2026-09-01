package maya

import (
   "41.neocities.org/diana/playReady"
   "41.neocities.org/diana/widevine"
   "41.neocities.org/sofia"
   "encoding/hex"
   "errors"
   "fmt"
   "log"
   "net/url"
   "os"
   "path/filepath"
   "strings"
)

// StopKey enables the interactive stop: press q (plus Enter on a normal
// terminal) to cleanly stop the download. Set to false when stdin is not a
// keyboard — tests, daemons, or programs that read stdin themselves.
// One download per process is assumed. Read once at download start.
var StopKey = true

func createFile(name string) (*os.File, error) {
   err := os.MkdirAll(filepath.Dir(name), os.ModePerm)
   if err != nil {
      return nil, err
   }
   log.Println("create:", name)
   return os.Create(name)
}

// orchestrateDownload contains the shared, high-level logic for executing any
// download. Pressing q stops the download cleanly (see StopKey); that is the
// only path that leaves a resumable state behind.
func orchestrateDownload(job *downloadJob) error {
   stop := stopSignal()
   if stop != nil {
      log.Println("press q (then Enter) to stop the download cleanly and save resume state")
   }

   var name strings.Builder
   name.WriteString(job.outputFileNameBase)
   name.WriteString(job.info.Extension)

   file, state, err := openOutput(name.String())
   if err != nil {
      return err
   }
   defer file.Close()

   var remux *sofia.Remuxer
   var key []byte
   if job.info.IsFmp4 {
      var initProtection *protectionInfo
      remux, initProtection, err = initializeRemuxer(job.initSegmentData, file, state)
      if err != nil {
         return err
      }

      if job.fetchKey != nil {
         key, err = getKeyForStream(job.fetchKey, job.manifestProtection, initProtection)
         if err != nil {
            return err
         }
      }
   }

   if len(state.records) > 0 {
      log.Printf("resume: skipping %d/%d already-downloaded segments", len(state.records), len(job.allRequests))
   }

   records, err := executeDownload(job.allRequests[len(state.records):], key, remux, file, job.threads, stop)

   switch {
   case errors.Is(err, ErrStopped):
      // The only path that produces a resume log.
      records = append(state.records, records...)
      log.Printf("stop: saving resume state for %d segments", len(records))
      return writeResumeLog(name.String()+".json", records)
   case err != nil:
      return err
   default:
      // Completed: clear any log left by a previous stop.
      if err := os.Remove(name.String() + ".json"); err != nil && !errors.Is(err, os.ErrNotExist) {
         return err
      }
      return nil
   }
}

// stopSignal starts a goroutine reading stdin for this download and returns
// its stop channel, or nil when StopKey is false. Selects on a nil channel
// block forever, so "disabled" means "never stop" with no other machinery.
func stopSignal() <-chan struct{} {
   if !StopKey {
      return nil
   }
   stop := make(chan struct{})
   go func() {
      buf := make([]byte, 1)
      for {
         n, err := os.Stdin.Read(buf)
         if err != nil || n == 0 {
            return
         }
         if buf[0] == 'q' {
            close(stop)
            return
         }
      }
   }()
   return stop
}

func initializeRemuxer(firstData []byte, file *os.File, state *resumeState) (*sofia.Remuxer, *protectionInfo, error) {
   var remux sofia.Remuxer
   remux.Writer = file
   if len(firstData) > 0 {
      var err error
      if len(state.records) > 0 {
         samples, chunkOffsets, samplesPerChunk := state.remuxState()
         err = remux.Resume(firstData, len(state.records), samples, chunkOffsets, samplesPerChunk)
      } else {
         err = remux.Initialize(firstData)
      }
      if err != nil {
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

      initProtection.KeyId = remux.Moov.FindDefaultKID()
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

// orchestrator.go

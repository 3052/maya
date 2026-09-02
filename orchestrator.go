package maya

import (
   "41.neocities.org/diana/playReady"
   "41.neocities.org/diana/widevine"
   "41.neocities.org/sofia"
   "encoding/binary"
   "encoding/hex"
   "errors"
   "fmt"
   "io"
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

// orchestrateDownload contains the shared, high-level logic for executing
// any download. Pressing q stops the download cleanly (see StopKey); for
// fMP4 streams the segment count and, when present, the decryption key are
// then written to the sidecar, and the sample tables are saved in the
// output file itself via the moov that the stop writes, so running the
// same command again resumes. Streams written without remuxing are not
// resumable.
func orchestrateDownload(job *downloadJob) error {
   stop := stopSignal()
   if stop != nil && job.info.IsFmp4 {
      log.Println("press q (then Enter) to stop the download cleanly and save resume state")
   }

   var name strings.Builder
   name.WriteString(job.outputFileNameBase)
   name.WriteString(job.info.Extension)
   outputName := name.String()

   file, state, err := openOutput(outputName)
   if err != nil {
      return err
   }
   defer file.Close()

   var remux *sofia.Remuxer
   var key []byte
   if job.info.IsFmp4 {
      var initProtection *protectionInfo
      remux, initProtection, err = initializeRemuxer(job.initSegmentData, file, state.Segments)
      if err != nil {
         return err
      }

      if job.fetchKey != nil {
         if len(state.Key) > 0 {
            key = state.Key
            log.Println("resume: using saved decryption key")
         } else {
            key, err = getKeyForStream(job.fetchKey, job.manifestProtection, initProtection)
            if err != nil {
               return err
            }
         }
      }
   }

   if state.Segments > 0 {
      log.Printf("resume: skipping %d/%d already-downloaded segments", state.Segments, len(job.allRequests))
   }

   segmentsDone, err := executeDownload(job.allRequests[state.Segments:], key, remux, file, job.threads, stop)
   switch {
   case errors.Is(err, ErrStopped):
      // The only path that writes the sidecar.
      total := state.Segments + segmentsDone
      if total > 0 {
         if err := writeResume(outputName+".resume", &resumeState{Segments: total, Key: key}); err != nil {
            return err
         }
      }
      log.Println("stop: resume state saved; run again to resume")
      return nil
   case err != nil:
      return err
   default:
      // Completed: clear the state left by a previous stop.
      if err := os.Remove(outputName + ".resume"); err != nil && !errors.Is(err, os.ErrNotExist) {
         return err
      }
      return nil
   }
}

// readStoppedMoov reads the moov that a previous stop appended after the
// media data, and returns it with the offset where the payloads end. Only
// the 16-byte mdat header and the moov bytes are read; the media data
// never enters memory.
func readStoppedMoov(file *os.File) ([]byte, int64, error) {
   header := make([]byte, 16)
   if _, err := file.ReadAt(header, 0); err != nil {
      return nil, 0, fmt.Errorf("failed to read mdat header: %w", err)
   }
   if string(header[4:8]) != "mdat" {
      return nil, 0, errors.New("output file does not contain an mdat box; delete it and the resume state to start over")
   }
   // Finish patched the extended size field with the full box size, which
   // is also the offset where the moov begins.
   payloadEnd := int64(binary.BigEndian.Uint64(header[8:16]))
   if payloadEnd < 16 {
      return nil, 0, errors.New("output file was not finalized; delete it and the resume state to start over")
   }
   fi, err := file.Stat()
   if err != nil {
      return nil, 0, err
   }
   if fi.Size() <= payloadEnd {
      return nil, 0, errors.New("output file has no moov to resume from; delete it and the resume state to start over")
   }
   moovData := make([]byte, fi.Size()-payloadEnd)
   if _, err := file.ReadAt(moovData, payloadEnd); err != nil {
      return nil, 0, err
   }
   return moovData, payloadEnd, nil
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

// initializeRemuxer prepares the remuxer, either fresh or resumed from the
// moov that a previous stop appended to the output file. The moov is read
// back with bounded memory — only its own bytes, never the media data —
// then truncated away so new fragments append exactly where the old ones
// ended.
func initializeRemuxer(initData []byte, file *os.File, segmentsDone int) (*sofia.Remuxer, *protectionInfo, error) {
   var remux sofia.Remuxer
   remux.Writer = file

   if segmentsDone == 0 {
      if len(initData) > 0 {
         if err := remux.Initialize(initData); err != nil {
            return nil, nil, err
         }
      }
   } else {
      moovData, payloadEnd, err := readStoppedMoov(file)
      if err != nil {
         return nil, nil, err
      }
      remuxState, err := sofia.StateFromMoov(moovData)
      if err != nil {
         return nil, nil, fmt.Errorf("failed to rebuild remux state from %s: %w", file.Name(), err)
      }
      if err := remux.AdoptState(initData, remuxState, segmentsDone); err != nil {
         return nil, nil, err
      }
      // Drop the moov so new fragments append at the payload boundary.
      if err := file.Truncate(payloadEnd); err != nil {
         return nil, nil, err
      }
      if _, err := file.Seek(payloadEnd, io.SeekStart); err != nil {
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

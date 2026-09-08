// orchestrator.go
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
// fMP4 streams the resume state and, when present, the decryption key are
// then written to the sidecar, and the sample tables are saved in the
// output file itself via the moov that the stop writes, so running the
// same command again resumes. Streams written without remuxing are not
// resumable.
//
// A single-URL stream (job.single, DASH SegmentBase) downloads with one
// request handed straight to the remuxer: a fresh run reads the moov
// from the stream itself and fetches its DRM key from that moov (via
// OnMoov) before any segment is decrypted, while a resumed run fetches
// the init bytes on demand — the resume offset is past the moov — and
// rebuilds its state with AdoptState. The sidecar records the input
// byte offset, so a resume re-requests from that offset.
func orchestrateDownload(job *downloadJob) error {
   if job.single != nil && !job.info.IsFmp4 {
      return errors.New("single-URL downloads require fMP4")
   }
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
      if job.single != nil && state.Segments == 0 {
         // Fresh single-URL download: no init request and no Initialize.
         // Process consumes the moov from the stream; OnMoov fetches the
         // DRM key — the KID lives in that moov — before the first
         // segment is decrypted. key is captured so the stop path can
         // save it to the sidecar.
         remux = &sofia.Remuxer{Writer: file}
         if job.fetchKey != nil {
            remux.OnMoov = func(moov *sofia.MoovBox) error {
               initProtection, protErr := extractProtection(moov)
               if protErr != nil {
                  return protErr
               }
               var keyErr error
               key, keyErr = getKeyForStream(job.fetchKey, job.manifestProtection, initProtection)
               if keyErr != nil {
                  return keyErr
               }
               return setDecrypt(remux, key)
            }
         }
      } else {
         initData := job.initSegmentData
         if job.single != nil {
            // Resumed single-URL download: the resume offset is past the
            // moov at the front of the stream, so the init bytes are
            // fetched now for AdoptState.
            if job.fetchInit == nil {
               return errors.New("resumed download requires an init fetcher")
            }
            initData, err = job.fetchInit()
            if err != nil {
               return err
            }
         }
         var initProtection *protectionInfo
         remux, initProtection, err = initializeRemuxer(initData, file, state.Segments)
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
         if err := setDecrypt(remux, key); err != nil {
            return err
         }
      }
   }

   if state.Segments > 0 {
      if job.single != nil {
         log.Printf("resume: continuing at byte %d", state.Offset)
      } else {
         log.Printf("resume: skipping %d/%d already-downloaded segments", state.Segments, len(job.allRequests))
      }
   }

   var runErr error
   segmentsDone := 0
   if job.single != nil {
      runErr = executeStream(*job.single, state.Offset, remux, stop)
   } else {
      segmentsDone, runErr = executeSegments(job.allRequests[state.Segments:], remux, file, stop)
   }
   switch {
   case errors.Is(runErr, errStopped):
      if !job.info.IsFmp4 {
         // Streams written without remuxing keep no resume state; the
         // partial output cannot be resumed and must be deleted.
         log.Println("stop: raw streams are not resumable; delete the output to start over")
         return nil
      }
      // The only path that writes the sidecar.
      resume := &resumeState{Key: key}
      if job.single != nil {
         // Progress is relative to the first byte this session read, so
         // add the offset this session started from.
         offset, segments := remux.Progress()
         resume.Segments = segments
         resume.Offset = state.Offset + offset
      } else {
         resume.Segments = state.Segments + segmentsDone
      }
      if resume.Segments > 0 {
         if err := writeResume(outputName+".json", resume); err != nil {
            return err
         }
         log.Println("stop: resume state saved; run again to resume")
         return nil
      }
      log.Println("stop: nothing downloaded yet; delete the output to start over")
      return nil
   case runErr != nil:
      return runErr
   default:
      // Completed: clear the state left by a previous stop.
      if err := os.Remove(outputName + ".json"); err != nil && !errors.Is(err, os.ErrNotExist) {
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

// extractProtection reads the DRM identifiers out of a moov: the content
// ID from the Widevine or PlayReady pssh, and the default KID from the
// track's tenc box.
func extractProtection(moov *sofia.MoovBox) (*protectionInfo, error) {
   if moov == nil {
      return nil, nil
   }
   protection := &protectionInfo{}
   wvIdBytes, err := hex.DecodeString(widevineSystemId)
   if err != nil {
      panic("failed to decode hardcoded widevine system id")
   }
   prIdBytes, err := hex.DecodeString(playReadySystemId)
   if err != nil {
      panic("failed to decode hardcoded playready system id")
   }

   if pssh, ok := moov.FindPssh(wvIdBytes); ok {
      if wvData, err := widevine.DecodePsshData(pssh.Data); err == nil {
         protection.ContentId = wvData.ContentId
      }
   }
   if protection.ContentId == nil {
      if pssh, ok := moov.FindPssh(prIdBytes); ok {
         wrm, err := playReady.ParsePro(pssh.Data)
         if err != nil {
            return nil, fmt.Errorf("failed to parse PlayReady PRO: %w", err)
         }
         if wrm.Data.CustomAttributes != nil {
            protection.ContentId = []byte(wrm.Data.CustomAttributes.ContentId)
         }
      }
   }

   protection.KeyId = moov.FindDefaultKID()
   return protection, nil
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
      var err error
      initProtection, err = extractProtection(remux.Moov)
      if err != nil {
         return nil, nil, err
      }
   }
   return &remux, initProtection, nil
}

// downloadJob holds all the extracted, manifest-agnostic information needed to run a download.
type downloadJob struct {
   outputFileNameBase string
   info               *typeInfo
   allRequests        []segment
   // single, when set, is the whole stream as one URL (DASH SegmentBase):
   // downloaded with one request handed straight to sofia.Process, and
   // resumable by byte offset.
   single *segment
   // fetchInit, when set, fetches the initialization bytes on demand. It
   // is used only by a resumed single-URL download, whose byte offset is
   // past the moov at the front of the stream; a fresh single-URL
   // download reads its moov from the stream itself, and per-URL
   // downloads fetch their init eagerly into initSegmentData.
   fetchInit          func() ([]byte, error)
   initSegmentData    []byte
   manifestProtection *protectionInfo
   fetchKey           keyFetcher
}

// segment represents a single chunk to be downloaded.
type segment struct {
   url      *url.URL
   headers  map[string]string
   duration float64
}

// typeInfo holds the determined properties of a media stream
type typeInfo struct {
   Extension string
   IsFmp4    bool
}

// orchestrator.go

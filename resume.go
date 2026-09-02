package maya

import (
   "encoding/json"
   "errors"
   "fmt"
   "os"
)

// writeResume replaces the sidecar atomically: a crash during the write
// leaves either the old or the new state, never a torn file.
func writeResume(path string, state resumeState) error {
   data, err := json.Marshal(state)
   if err != nil {
      return err
   }
   tmp := path + ".tmp"
   if err := os.WriteFile(tmp, data, 0o666); err != nil {
      return err
   }
   return os.Rename(tmp, path)
}

// The resume state is a constant-size sidecar written next to the output
// file (<output>.resume). For fMP4 streams it records only the segment
// count: the sample tables needed to resume live in the moov that a stop
// appends to the output file itself, and sofia.StateFromMoov rebuilds the
// remuxer state from them. For streams written without remuxing there are
// no tables, so the byte count is recorded instead and the file is simply
// truncated back to it. The sidecar is replaced atomically after every
// segment, so a crash at any point resumes from the last full segment.
type resumeState struct {
   Segments int   `json:"segments"`
   Bytes    int64 `json:"bytes"`
}

// openOutput opens the output file for writing, resuming from an existing
// resume state when possible. Trimming the stopped file back to its
// payload boundary happens later and elsewhere: for fMP4 streams in
// initializeRemuxer (after the moov has been read back), and for raw
// streams in orchestrateDownload (via the recorded byte count).
func openOutput(name string) (*os.File, *resumeState, error) {
   state, err := readResume(name + ".resume")
   if err != nil {
      return nil, nil, err
   }
   if state == nil {
      if fi, err := os.Stat(name); err == nil && fi.Size() > 0 {
         return nil, nil, fmt.Errorf("%s already exists and no resume state was found; delete it to start over", name)
      }
      file, err := createFile(name)
      return file, &resumeState{}, err
   }
   file, err := os.OpenFile(name, os.O_RDWR, 0)
   if err != nil {
      return nil, nil, fmt.Errorf("resume state exists but %s cannot be opened; delete the state to start over", name)
   }
   return file, state, nil
}

// readResume parses the sidecar. A nil state with no error means none
// exists.
func readResume(path string) (*resumeState, error) {
   data, err := os.ReadFile(path)
   if errors.Is(err, os.ErrNotExist) {
      return nil, nil
   }
   if err != nil {
      return nil, err
   }
   var state resumeState
   if err := json.Unmarshal(data, &state); err != nil {
      return nil, fmt.Errorf("resume state %s is corrupt; delete it to start over", path)
   }
   if state.Segments < 0 || state.Bytes < 0 {
      return nil, fmt.Errorf("resume state %s is corrupt; delete it to start over", path)
   }
   return &state, nil
}

// resume.go

package maya

import (
   "encoding/json"
   "errors"
   "fmt"
   "os"
)

// writeResume writes the sidecar in a single short write.
func writeResume(path string, state resumeState) error {
   data, err := json.Marshal(state)
   if err != nil {
      return err
   }
   return os.WriteFile(path, data, 0o666)
}

// The resume state is a sidecar written next to the output file
// (<output>.resume) for fMP4 streams only, written once when the download
// is stopped cleanly. It records the segment count and, when the stream
// was decrypted, the key, so a resume does not have to fetch a license
// again. The sample tables needed to resume live in the moov that the
// stop appends to the output file itself, and sofia.StateFromMoov
// rebuilds the remuxer state from them. Streams written without remuxing
// are not resumable and get no sidecar.

type resumeState struct {
   Segments int    `json:"segments"`
   Key      []byte `json:"key,omitempty"`
}

// openOutput opens the output file for writing, resuming from an existing
// resume state when possible. Trimming a stopped fMP4 file back to its
// payload boundary happens in initializeRemuxer, after the moov has been
// read back.
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
   if state.Segments < 0 {
      return nil, fmt.Errorf("resume state %s is corrupt; delete it to start over", path)
   }
   return &state, nil
}

// resume.go

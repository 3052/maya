package maya

import (
   "errors"
   "fmt"
   "os"
   "strconv"
)

// openOutput opens the output file for writing, resuming from an existing
// resume state when possible. Trimming a stopped fMP4 file back to its
// payload boundary happens in initializeRemuxer, after the moov has been
// read back.
func openOutput(name string) (*os.File, *int, error) {
   segments, err := readResume(name + ".resume")
   if err != nil {
      return nil, nil, err
   }
   if segments == nil {
      if fi, err := os.Stat(name); err == nil && fi.Size() > 0 {
         return nil, nil, fmt.Errorf("%s already exists and no resume state was found; delete it to start over", name)
      }
      file, err := createFile(name)
      return file, new(int), err
   }
   file, err := os.OpenFile(name, os.O_RDWR, 0)
   if err != nil {
      return nil, nil, fmt.Errorf("resume state exists but %s cannot be opened; delete the state to start over", name)
   }
   return file, segments, nil
}

// The resume state is a one-number sidecar written next to the output file
// (<output>.resume) for fMP4 streams only. It records the segment count as
// plain text; the sample tables needed to resume live in the moov that a
// stop appends to the output file itself, and sofia.StateFromMoov rebuilds
// the remuxer state from them. The sidecar is replaced after every segment,
// so a crash at any point resumes from the last full segment. Streams
// written without remuxing are not resumable and get no sidecar.

// readResume parses the sidecar. A nil state with no error means none
// exists.
func readResume(path string) (*int, error) {
   data, err := os.ReadFile(path)
   if errors.Is(err, os.ErrNotExist) {
      return nil, nil
   }
   if err != nil {
      return nil, err
   }
   segments, err := strconv.Atoi(string(data))
   if err != nil || segments < 0 {
      return nil, fmt.Errorf("resume state %s is corrupt; delete it to start over", path)
   }
   return &segments, nil
}

// writeResume replaces the sidecar with the current segment count. It is a
// single short write, so a crash during it leaves either the old or the
// new count, never anything worth recovering.
func writeResume(path string, segments int) error {
   return os.WriteFile(path, []byte(strconv.Itoa(segments)), 0o666)
}

// resume.go

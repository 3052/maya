package maya

import (
   "41.neocities.org/sofia"
   "encoding/json"
   "errors"
   "fmt"
   "io"
   "log"
   "math"
   "os"
)

// The resume log is a JSON file written next to the output file when a download
// is cleanly stopped (<output>.json). It is written once, in a single
// operation, so an interrupted write leaves a corrupt log, which readResumeLog
// rejects with a clear error. It records every fully-written segment so the
// stopped download can continue with the remaining segments, and is deleted
// once the download completes. Resuming assumes the segment list is identical
// to the previous run.
//
// The file is a single JSON array of records:
//    [[endOffset, chunks, samples], ...]
// where chunks is a list of [offset, count] pairs and samples is a list of
// [duration, size, sync, compositionOffset] tuples, with sync as 0 or 1.
// Both lists are empty for streams written without remuxing.

// encodeRecord converts one segment record to its JSON row form:
// [endOffset, chunks, samples].
func encodeRecord(rec *segmentRecord) []any {
   chunks := [][2]uint64{}
   samples := [][4]int64{}
   if rec.appended != nil {
      chunks = make([][2]uint64, len(rec.appended.ChunkOffsets))
      for i, offset := range rec.appended.ChunkOffsets {
         chunks[i] = [2]uint64{offset, uint64(rec.appended.SamplesPerChunk[i])}
      }
      samples = make([][4]int64, len(rec.appended.Samples))
      for i, sample := range rec.appended.Samples {
         var sync int64
         if sample.IsSync {
            sync = 1
         }
         samples[i] = [4]int64{int64(sample.Duration), int64(sample.Size), sync, int64(sample.CompositionTimeOffset)}
      }
   }
   return []any{rec.endOffset, chunks, samples}
}

// writeResumeLog writes the resume log for a cleanly stopped download in a
// single write. os.Create truncates any log left by a previous stop.
func writeResumeLog(path string, records []segmentRecord) error {
   rows := make([]any, len(records))
   for i := range records {
      rows[i] = encodeRecord(&records[i])
   }
   data, err := json.Marshal(rows)
   if err != nil {
      return err
   }

   file, err := os.Create(path)
   if err != nil {
      return err
   }
   if _, err := file.Write(data); err != nil {
      file.Close()
      return err
   }
   return file.Close()
}

// resumeState is the parsed resume log.
type resumeState struct {
   records []segmentRecord
}

// openOutput opens the output file for writing, resuming from an existing
// resume log when possible. It returns the file positioned where the next
// segment should be written, along with the valid resume state (which may
// have no records).
func openOutput(name string) (*os.File, *resumeState, error) {
   state, err := readResumeLog(name + ".json")
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
      log.Printf("resume: cannot open %s; starting over", name)
      file, err := createFile(name)
      return file, &resumeState{}, err
   }
   fi, err := file.Stat()
   if err != nil {
      file.Close()
      return nil, nil, err
   }
   boundary := state.boundary()
   if fi.Size() < boundary {
      file.Close()
      log.Printf("resume: %s is smaller than the resume state; starting over", name)
      file, err = createFile(name)
      return file, &resumeState{}, err
   }
   if fi.Size() > boundary {
      // A cleanly stopped download ends with a finalized moov past the last
      // recorded segment. Either way, bytes beyond the boundary are not part
      // of the resume state.
      log.Printf("resume: discarding %d trailing bytes from %s", fi.Size()-boundary, name)
      if err := file.Truncate(boundary); err != nil {
         file.Close()
         return nil, nil, err
      }
   }
   if _, err := file.Seek(0, io.SeekEnd); err != nil {
      file.Close()
      return nil, nil, err
   }
   return file, state, nil
}

// readResumeLog parses the resume log. A nil state with no error means no log
// exists. The log is written in a single operation, so any parse failure is
// genuine corruption and an error is returned.
func readResumeLog(path string) (*resumeState, error) {
   data, err := os.ReadFile(path)
   if errors.Is(err, os.ErrNotExist) {
      return nil, nil
   }
   if err != nil {
      return nil, err
   }

   var rows []json.RawMessage
   if err := json.Unmarshal(data, &rows); err != nil {
      return nil, fmt.Errorf("resume log %s is corrupt; delete it to start over", path)
   }

   state := &resumeState{}
   for _, row := range rows {
      var fields []json.RawMessage
      if err := json.Unmarshal(row, &fields); err != nil {
         return nil, fmt.Errorf("resume log %s is corrupt; delete it to start over", path)
      }
      rec, ok := decodeRecord(fields)
      if !ok {
         return nil, fmt.Errorf("resume log %s is corrupt; delete it to start over", path)
      }
      state.records = append(state.records, *rec)
   }
   return state, nil
}

func (s *resumeState) boundary() int64 {
   if len(s.records) == 0 {
      return 0
   }
   return int64(s.records[len(s.records)-1].endOffset)
}

// remuxState rebuilds the Remuxer bookkeeping for the fragments already
// present in the file.
func (s *resumeState) remuxState() ([]*sofia.RemuxSample, []uint64, []uint32) {
   var samples []*sofia.RemuxSample
   var chunkOffsets []uint64
   var samplesPerChunk []uint32
   for i := range s.records {
      rec := &s.records[i]
      if rec.appended == nil {
         continue
      }
      chunkOffsets = append(chunkOffsets, rec.appended.ChunkOffsets...)
      samplesPerChunk = append(samplesPerChunk, rec.appended.SamplesPerChunk...)
      samples = append(samples, rec.appended.Samples...)
   }
   return samples, chunkOffsets, samplesPerChunk
}

// segmentRecord describes one fully-written segment. appended is nil for
// streams that are written without remuxing.
type segmentRecord struct {
   endOffset uint64
   appended  *sofia.AppendResult
}

func decodeRecord(fields []json.RawMessage) (*segmentRecord, bool) {
   if len(fields) != 3 {
      return nil, false
   }

   var endOffset uint64
   if err := json.Unmarshal(fields[0], &endOffset); err != nil {
      return nil, false
   }
   rec := &segmentRecord{endOffset: endOffset}

   // Fixed-size arrays enforce the element counts at unmarshal time:
   // a wrong-length pair or tuple fails here instead of needing a manual
   // length check.
   var chunkPairs [][2]uint64
   if err := json.Unmarshal(fields[1], &chunkPairs); err != nil {
      return nil, false
   }
   var sampleTuples [][4]int64
   if err := json.Unmarshal(fields[2], &sampleTuples); err != nil {
      return nil, false
   }
   if len(chunkPairs) == 0 && len(sampleTuples) == 0 {
      return rec, true
   }
   rec.appended = &sofia.AppendResult{
      ChunkOffsets:    make([]uint64, len(chunkPairs)),
      SamplesPerChunk: make([]uint32, len(chunkPairs)),
      Samples:         make([]*sofia.RemuxSample, len(sampleTuples)),
   }

   var total uint32
   for i := range chunkPairs {
      if chunkPairs[i][1] > math.MaxUint32 {
         return nil, false
      }
      rec.appended.ChunkOffsets[i] = chunkPairs[i][0]
      rec.appended.SamplesPerChunk[i] = uint32(chunkPairs[i][1])
      total += uint32(chunkPairs[i][1])
   }
   for i := range sampleTuples {
      duration := sampleTuples[i][0]
      size := sampleTuples[i][1]
      sync := sampleTuples[i][2]
      cto := sampleTuples[i][3]
      if duration < 0 || duration > math.MaxUint32 ||
         size < 0 || size > math.MaxUint32 ||
         cto < math.MinInt32 || cto > math.MaxInt32 ||
         (sync != 0 && sync != 1) {
         return nil, false
      }
      rec.appended.Samples[i] = &sofia.RemuxSample{
         Duration:              uint32(duration),
         Size:                  uint32(size),
         IsSync:                sync == 1,
         CompositionTimeOffset: int32(cto),
      }
   }

   // sanity: the per-chunk sample counts must add up to the sample list
   if int(total) != len(rec.appended.Samples) {
      return nil, false
   }
   return rec, true
}

// resume.go

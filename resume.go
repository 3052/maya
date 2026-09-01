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

func encodeRecord(rec *segmentRecord) ([]byte, error) {
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
   return json.Marshal([]any{rec.endOffset, chunks, samples})
}

// resumeLog appends segment records to the resume log.
type resumeLog struct {
   file *os.File
}

func createResumeLog(path string, replay []segmentRecord) (*resumeLog, error) {
   file, err := os.Create(path)
   if err != nil {
      return nil, err
   }
   log.Println("create:", path)

   l := &resumeLog{file: file}
   for i := range replay {
      if err := l.record(&replay[i]); err != nil {
         l.file.Close()
         return nil, err
      }
   }
   return l, nil
}

func (l *resumeLog) record(rec *segmentRecord) error {
   data, err := encodeRecord(rec)
   if err != nil {
      return err
   }
   data = append(data, '\n')
   _, err = l.file.Write(data)
   return err
}

// resumeState is the parsed resume log.
type resumeState struct {
   records []segmentRecord
}

// The resume log is a JSON Lines file written next to the output file while a
// download runs (<output>.json). Each line is one completed segment, written as
// a single compact JSON array so an interrupted write corrupts only that one
// line. It records every fully-written segment so an interrupted download can
// continue with the remaining segments, and is deleted once the download
// completes. Resuming assumes the segment list is identical to the previous run.
//
// Each line is:
//    [endOffset, chunks, samples]
// where chunks is a list of [offset, count] pairs and samples is a list of
// [duration, size, sync, compositionOffset] tuples, with sync as 0 or 1.
// Both lists are empty for streams written without remuxing.

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
      log.Printf("resume: discarding %d partially written bytes from %s", fi.Size()-boundary, name)
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

func readResumeLog(path string) (*resumeState, error) {
   file, err := os.Open(path)
   if errors.Is(err, os.ErrNotExist) {
      return nil, nil
   }
   if err != nil {
      return nil, err
   }
   defer file.Close()

   reader := json.NewDecoder(file)

   state := &resumeState{}
   for {
      var fields []json.RawMessage
      err := reader.Decode(&fields)
      if err == io.EOF {
         break
      }
      if err != nil {
         // A torn trailing line means the last segment was not fully
         // recorded; it will simply be downloaded again.
         log.Printf("resume: ignoring trailing corrupt data in %s", path)
         break
      }
      rec, ok := decodeRecord(fields)
      if !ok {
         log.Printf("resume: ignoring trailing corrupt data in %s", path)
         break
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

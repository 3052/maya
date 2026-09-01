package maya

import (
   "41.neocities.org/sofia"
   "encoding/csv"
   "errors"
   "fmt"
   "io"
   "log"
   "os"
   "strconv"
   "strings"
)

func encodeRecord(rec *segmentRecord) []string {
   var chunkValues []string
   var sampleValues []string
   if rec.appended != nil {
      for i, offset := range rec.appended.ChunkOffsets {
         chunkValues = append(chunkValues,
            strconv.FormatUint(offset, 10),
            strconv.FormatUint(uint64(rec.appended.SamplesPerChunk[i]), 10))
      }
      for i := range rec.appended.Samples {
         sample := &rec.appended.Samples[i]
         sync := "0"
         if sample.IsSync {
            sync = "1"
         }
         sampleValues = append(sampleValues,
            strconv.FormatUint(uint64(sample.Duration), 10),
            strconv.FormatUint(uint64(sample.Size), 10),
            sync,
            strconv.FormatInt(int64(sample.CompositionTimeOffset), 10))
      }
   }
   return []string{
      strconv.FormatUint(rec.endOffset, 10),
      strings.Join(chunkValues, ";"),
      strings.Join(sampleValues, ";"),
   }
}

func splitList(s string) []string {
   if s == "" {
      return nil
   }
   return strings.Split(s, ";")
}

// resumeLog appends segment records to the resume log.
type resumeLog struct {
   file *os.File
   enc  *csv.Writer
}

func createResumeLog(path string, replay []segmentRecord) (*resumeLog, error) {
   file, err := os.Create(path)
   if err != nil {
      return nil, err
   }
   log.Println("create:", path)

   l := &resumeLog{file: file, enc: csv.NewWriter(file)}
   for i := range replay {
      if err := l.record(&replay[i]); err != nil {
         l.file.Close()
         return nil, err
      }
   }
   return l, nil
}

func (l *resumeLog) record(rec *segmentRecord) error {
   if err := l.enc.Write(encodeRecord(rec)); err != nil {
      return err
   }
   l.enc.Flush()
   return l.enc.Error()
}

// resumeState is the parsed resume log.
type resumeState struct {
   records []segmentRecord
}

// The resume log is a CSV file written next to the output file while a
// download runs (<output>.csv). It records every fully-written segment so
// an interrupted download can continue with the remaining segments, and is
// deleted once the download completes. Resuming assumes the segment list is
// identical to the previous run.
//
// Layout (one CSV row per line):
//    endOffset, chunks, samples
// where chunks is a semicolon-separated list of "offset;count" pairs and
// samples is a semicolon-separated list of
// "duration;size;sync;compositionOffset" groups. Both list fields are empty
// for streams written without remuxing.

// openOutput opens the output file for writing, resuming from an existing
// resume log when possible. It returns the file positioned where the next
// segment should be written, along with the valid resume state (which may
// have no records).
func openOutput(name string) (*os.File, *resumeState, error) {
   state, err := readResumeLog(name + ".csv")
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

   reader := csv.NewReader(file)
   reader.FieldsPerRecord = -1

   state := &resumeState{}
   for {
      fields, err := reader.Read()
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
func (s *resumeState) remuxState() ([]sofia.RemuxSample, []uint64, []uint32) {
   var samples []sofia.RemuxSample
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

func decodeRecord(fields []string) (*segmentRecord, bool) {
   if len(fields) != 3 {
      return nil, false
   }
   endOffset, err := strconv.ParseUint(fields[0], 10, 64)
   if err != nil {
      return nil, false
   }
   rec := &segmentRecord{endOffset: endOffset}

   chunkValues := splitList(fields[1])
   if len(chunkValues)%2 != 0 {
      return nil, false
   }
   sampleValues := splitList(fields[2])
   if len(sampleValues)%4 != 0 {
      return nil, false
   }
   if len(chunkValues) > 0 || len(sampleValues) > 0 {
      rec.appended = &sofia.AppendResult{}
   }

   var total uint32
   for i := 0; i < len(chunkValues); i += 2 {
      offset, err := strconv.ParseUint(chunkValues[i], 10, 64)
      if err != nil {
         return nil, false
      }
      count, err := strconv.ParseUint(chunkValues[i+1], 10, 32)
      if err != nil {
         return nil, false
      }
      rec.appended.ChunkOffsets = append(rec.appended.ChunkOffsets, offset)
      rec.appended.SamplesPerChunk = append(rec.appended.SamplesPerChunk, uint32(count))
      total += uint32(count)
   }
   for i := 0; i < len(sampleValues); i += 4 {
      duration, err := strconv.ParseUint(sampleValues[i], 10, 32)
      if err != nil {
         return nil, false
      }
      size, err := strconv.ParseUint(sampleValues[i+1], 10, 32)
      if err != nil {
         return nil, false
      }
      sync := sampleValues[i+2]
      if sync != "0" && sync != "1" {
         return nil, false
      }
      cto, err := strconv.ParseInt(sampleValues[i+3], 10, 32)
      if err != nil {
         return nil, false
      }
      rec.appended.Samples = append(rec.appended.Samples, sofia.RemuxSample{
         Duration:              uint32(duration),
         Size:                  uint32(size),
         IsSync:                sync == "1",
         CompositionTimeOffset: int32(cto),
      })
   }

   // sanity: the per-chunk sample counts must add up to the sample list
   if rec.appended != nil && int(total) != len(rec.appended.Samples) {
      return nil, false
   }
   return rec, true
}

// resume.go

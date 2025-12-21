package maya

import (
   "41.neocities.org/dash"
   "41.neocities.org/sofia"
   "crypto/aes"
   "log"
   "os"
   "strings"
   "sync"
)

func createOutputFile(rep *dash.Representation) (*os.File, error) {
   mime := rep.GetMimeType()
   parts := strings.Split(mime, "/")
   if len(parts) != 2 {
      return nil, new_error("invalid mime type:", mime)
   }
   extension := "." + parts[1]
   if mime == "audio/mp4" {
      extension = ".m4a"
   }
   name := rep.Id + extension
   log.Println("Create", name)
   return os.Create(name)
}

func (c *Config) downloadGroup(group []*dash.Representation) error {
   rep := group[0]
   var media mediaFile
   // Configure PSSH if available in MPD
   if err := media.configureProtection(rep); err != nil {
      return err
   }
   file, err := createOutputFile(rep)
   if err != nil {
      return err
   }
   defer file.Close()
   // Download raw init segment
   initData, err := c.downloadInitialization(&media, rep)
   if err != nil {
      return err
   }
   // Initialize Remuxer and parse Moov (in place) to get DRM/Timescale info
   remux, err := media.initializeWriter(file, initData)
   if err != nil {
      return err
   }
   // Fetch key using info extracted from MPD or Init Segment
   key, err := c.fetchKey(&media)
   if err != nil {
      return err
   }
   // getMediaRequests now only returns requests (and error)
   requests, err := getMediaRequests(group)
   if err != nil {
      return err
   }
   if len(requests) == 0 {
      return nil
   }
   numWorkers := c.Threads
   if numWorkers < 1 {
      numWorkers = 1
   }
   jobs := make(chan job, len(requests))
   results := make(chan result, len(requests))
   var wg sync.WaitGroup
   // Start Workers
   wg.Add(numWorkers)
   for workerId := 0; workerId < numWorkers; workerId++ {
      go func(id int) {
         defer wg.Done()
         for downloadJob := range jobs {
            data, err := getSegment(downloadJob.request.url, downloadJob.request.header)
            results <- result{index: downloadJob.index, workerId: id, data: data, err: err}
         }
      }(workerId)
   }
   // Start Writer (processes results)
   doneChan := make(chan error, 1)
   go media.processAndWriteSegments(doneChan, results, len(requests), numWorkers, key, remux)
   // Send Jobs
   for reqIndex, req := range requests {
      jobs <- job{index: reqIndex, request: req}
   }
   close(jobs)
   // Wait for writer to finish
   if err := <-doneChan; err != nil {
      return err
   }
   return nil
}

func (m *mediaFile) initializeWriter(file *os.File, initData []byte) (*sofia.Remuxer, error) {
   var remux sofia.Remuxer
   remux.Writer = file
   if len(initData) > 0 {
      // Initialize parses the init segment and sets remux.Moov
      if err := remux.Initialize(initData); err != nil {
         return nil, err
      }
      // Combined Logic from configureMoov:
      // Handle Widevine PSSH logic
      // Optimization: Only search atoms and parse if we don't already have the ContentId
      if m.content_id == nil {
         if wvBox, ok := remux.Moov.FindPssh(widevineId); ok {
            if err := m.ingestWidevinePssh(wvBox.Data); err != nil {
               return nil, err
            }
         }
      }
      // Cleanup atoms
      remux.Moov.RemovePssh()
   }
   return &remux, nil
}

func (m *mediaFile) processAndWriteSegments(
   doneChan chan<- error,
   results <-chan result,
   totalSegments int,
   numWorkers int,
   key []byte,
   remux *sofia.Remuxer,
) {
   // Setup Decryption Block once if key is present
   if len(key) > 0 {
      block, err := aes.NewCipher(key)
      if err != nil {
         doneChan <- err
         return
      }
      // Decrypt samples in place using the block
      remux.OnSample = func(sample []byte, info *sofia.SampleEncryptionInfo) {
         sofia.DecryptSample(sample, info, block)
      }
   }
   // Setup Progress Tracking
   prog := newProgress(totalSegments, numWorkers)
   // Store full result to keep track of workerId
   pending := make(map[int]result)
   nextIndex := 0
   for segmentIndex := 0; segmentIndex < totalSegments; segmentIndex++ {
      res := <-results
      if res.err != nil {
         doneChan <- res.err
         return
      }
      pending[res.index] = res
      // Write all available sequential segments
      for {
         item, ok := pending[nextIndex]
         if !ok {
            break
         }
         // AddSegment decrypts samples, writes mdat payload to file, and triggers OnSampleInfo
         if err := remux.AddSegment(item.data); err != nil {
            doneChan <- err
            return
         }
         // Update progress using the worker ID that downloaded this segment
         prog.update(item.workerId)
         delete(pending, nextIndex)
         nextIndex++
      }
   }
   // Finish writes the final moov box and updates mdat size
   if err := remux.Finish(); err != nil {
      doneChan <- err
      return
   }
   doneChan <- nil
}

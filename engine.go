package net

import (
   "41.neocities.org/dash"
   "41.neocities.org/drm/widevine"
   "41.neocities.org/sofia"
   "crypto/aes"
   "log"
   "os"
   "sync"
)

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

   // Initialize Unfragmenter and parse Moov (in place) to get DRM/Timescale info
   unfrag, err := media.initializeWriter(file, initData)
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
   for workerID := 0; workerID < numWorkers; workerID++ {
      go func(id int) {
         defer wg.Done()
         for downloadJob := range jobs {
            data, err := getSegment(downloadJob.request.url, downloadJob.request.header)
            results <- result{index: downloadJob.index, workerID: id, data: data, err: err}
         }
      }(workerID)
   }

   // Start Writer (processes results)
   doneChan := make(chan error, 1)
   go media.processAndWriteSegments(doneChan, results, len(requests), numWorkers, key, unfrag)

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

func (m *mediaFile) initializeWriter(file *os.File, initData []byte) (*sofia.Unfragmenter, error) {
   var unfrag sofia.Unfragmenter
   unfrag.Writer = file

   if len(initData) > 0 {
      // Initialize parses the init segment and sets unfrag.Moov
      if err := unfrag.Initialize(initData); err != nil {
         return nil, err
      }

      // Combined Logic from configureMoov:
      // Handle Widevine PSSH
      if wvBox, ok := unfrag.Moov.FindPssh(widevineID); ok {
         var pssh_data widevine.PsshData
         if err := pssh_data.Unmarshal(wvBox.Data); err != nil {
            return nil, err
         }
         if pssh_data.ContentID != nil {
            m.content_id = pssh_data.ContentID
            log.Printf("MP4 content ID %x", m.content_id)
         }
      }
      // Cleanup atoms
      unfrag.Moov.RemovePssh()
   }

   return &unfrag, nil
}

func (m *mediaFile) processAndWriteSegments(
   doneChan chan<- error,
   results <-chan result,
   totalSegments int,
   numWorkers int,
   key []byte,
   unfrag *sofia.Unfragmenter,
) {
   // Setup Decryption Block once if key is present
   if len(key) > 0 {
      block, err := aes.NewCipher(key)
      if err != nil {
         doneChan <- err
         return
      }
      // Decrypt samples in place using the block
      unfrag.OnSample = func(sample []byte, info *sofia.SampleEncryptionInfo) {
         sofia.DecryptSample(sample, info, block)
      }
   }

   // Setup Progress Tracking
   prog := newProgress(totalSegments, numWorkers)

   // Store full result to keep track of workerID
   pending := make(map[int]result)
   nextIndex := 0

   for i := 0; i < totalSegments; i++ {
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
         if err := unfrag.AddSegment(item.data); err != nil {
            doneChan <- err
            return
         }

         // Update progress using the worker ID that downloaded this segment
         prog.update(item.workerID)

         delete(pending, nextIndex)
         nextIndex++
      }
   }

   // Finish writes the final moov box and updates mdat size
   if err := unfrag.Finish(); err != nil {
      doneChan <- err
      return
   }
   doneChan <- nil
}

func createOutputFile(rep *dash.Representation) (*os.File, error) {
   extension := ".mp4"
   switch rep.GetMimeType() {
   case "audio/mp4":
      extension = ".m4a"
   case "text/vtt":
      extension = ".vtt"
   case "video/mp4":
      extension = ".m4v"
   }
   name := rep.ID + extension
   log.Println("Create", name)
   return os.Create(name)
}

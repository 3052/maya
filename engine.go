package maya

import (
   "41.neocities.org/dash"
   "41.neocities.org/sofia"
   "crypto/aes"
   "io"
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

   // Determine if content is MP4 (needs remuxing/init)
   isMp4 := strings.Contains(rep.GetMimeType(), "mp4")
   var remux *sofia.Remuxer

   // Only MP4s have Initialization segments and need Remuxer setup
   if isMp4 {
      initData, err := c.downloadInitialization(&media, rep)
      if err != nil {
         return err
      }
      remux, err = media.initializeWriter(file, initData)
      if err != nil {
         return err
      }
   }

   // Fetch key (only used for MP4 decryption logic here)
   key, err := c.fetchKey(&media)
   if err != nil {
      return err
   }

   requests, err := getMediaRequests(group)
   if err != nil {
      return err
   }

   if len(requests) == 0 {
      return nil
   }

   numWorkers := max(c.Threads, 1)

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
   go media.processAndWriteSegments(doneChan, results, len(requests), numWorkers, key, remux, file)

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
      if err := remux.Initialize(initData); err != nil {
         return nil, err
      }
      if m.content_id == nil {
         if wvBox, ok := remux.Moov.FindPssh(widevineId); ok {
            if err := m.ingestWidevinePssh(wvBox.Data); err != nil {
               return nil, err
            }
         }
      }
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
   dst io.Writer,
) {
   // 1. Setup Decryption (Only for MP4 Remuxing)
   if remux != nil && len(key) > 0 {
      block, err := aes.NewCipher(key)
      if err != nil {
         doneChan <- err
         return
      }
      remux.OnSample = func(sample []byte, info *sofia.SampleEncryptionInfo) {
         sofia.DecryptSample(sample, info, block)
      }
   }

   prog := newProgress(totalSegments, numWorkers)
   pending := make(map[int]result)
   nextIndex := 0

   for segmentIndex := 0; segmentIndex < totalSegments; segmentIndex++ {
      res := <-results
      if res.err != nil {
         doneChan <- res.err
         return
      }
      pending[res.index] = res

      for {
         item, ok := pending[nextIndex]
         if !ok {
            break
         }

         // 2. Write Logic: Remux MP4 or Write Raw for generic files
         if remux != nil {
            if err := remux.AddSegment(item.data); err != nil {
               doneChan <- err
               return
            }
         } else {
            if _, err := dst.Write(item.data); err != nil {
               doneChan <- err
               return
            }
         }

         prog.update(item.workerId)
         delete(pending, nextIndex)
         nextIndex++
      }
   }

   // 3. Finalize
   if remux != nil {
      if err := remux.Finish(); err != nil {
         doneChan <- err
         return
      }
   }
   doneChan <- nil
}

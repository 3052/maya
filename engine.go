package maya

import (
   "41.neocities.org/sofia"
   "crypto/aes"
   "fmt"
   "io"
   "log"
   "net/http"
   "net/url"
   "os"
   "strings"
   "sync"
)

// Internal types for the worker pool
type mediaRequest struct {
   url    *url.URL
   header http.Header
}
type job struct {
   index   int
   request mediaRequest
}
type result struct {
   index    int
   workerId int
   data     []byte
   err      error
}

func createOutputFile(id string, mime string) (*os.File, error) {
   parts := strings.Split(mime, "/")
   if len(parts) != 2 {
      return nil, fmt.Errorf("invalid mime type %v", mime)
   }
   extension := "." + parts[1]
   if mime == "audio/mp4" {
      extension = ".m4a"
   }
   if mime == "video/mp2t" {
      extension = ".ts"
   }
   name := id + extension
   log.Println("Create", name)
   return os.Create(name)
}

// downloadGroupInternal is the shared engine that works with the stream abstraction.
func (c *Config) downloadGroupInternal(group streamGroup) error {
   if len(group) == 0 {
      return fmt.Errorf("cannot download empty stream group")
   }
   rep := group[0]
   var media mediaFile
   protections, err := rep.getProtection()
   if err != nil {
      return err
   }
   if err := media.configureProtection(protections); err != nil {
      return err
   }
   file, err := createOutputFile(rep.getID(), rep.getMimeType())
   if err != nil {
      return err
   }
   defer file.Close()
   isMp4 := strings.Contains(rep.getMimeType(), "mp4")
   var remux *sofia.Remuxer
   if isMp4 {
      initSeg, err := rep.getInitSegment()
      if err != nil {
         return err
      }
      var initData []byte
      if initSeg != nil {
         initData, err = getSegment(initSeg.url, initSeg.header)
         if err != nil {
            return err
         }
      }
      remux, err = media.initializeWriter(file, initData)
      if err != nil {
         return err
      }
   }
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
   doneChan := make(chan error, 1)
   go media.processAndWriteSegments(doneChan, results, len(requests), numWorkers, key, remux, file)
   for reqIndex, req := range requests {
      jobs <- job{index: reqIndex, request: req}
   }
   close(jobs)
   if err := <-doneChan; err != nil {
      return err
   }
   return nil
}

// getMediaRequests is now fully generic. It iterates through a group of streams
// and asks each one to provide its list of downloadable segments.
func getMediaRequests(group streamGroup) ([]mediaRequest, error) {
   var requests []mediaRequest
   for _, s := range group {
      // Delegate responsibility to the stream itself.
      segments, err := s.getSegments()
      if err != nil {
         return nil, err
      }
      for _, seg := range segments {
         requests = append(requests, mediaRequest{url: seg.url, header: seg.header})
      }
   }
   return requests, nil
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
   if remux != nil {
      if err := remux.Finish(); err != nil {
         doneChan <- err
         return
      }
   }
   doneChan <- nil
}

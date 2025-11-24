package net

import (
   "41.neocities.org/dash"
   "41.neocities.org/drm/playReady"
   "41.neocities.org/drm/widevine"
   "41.neocities.org/sofia"
   "bytes"
   "fmt"
   "io"
   "log"
   "math/big"
   "net/http"
   "net/url"
   "os"
   "strings"
   "sync"
)

func (c *Config) widevineKey(media *MediaFile) ([]byte, error) {
   client_id, err := os.ReadFile(c.ClientId)
   if err != nil {
      return nil, err
   }
   pemBytes, err := os.ReadFile(c.PrivateKey)
   if err != nil {
      return nil, err
   }
   var pssh widevine.PsshData
   pssh.ContentID = media.content_id
   pssh.KeyIDs = [][]byte{media.key_id}
   req_bytes, err := pssh.BuildLicenseRequest(client_id)
   if err != nil {
      return nil, err
   }
   privateKey, err := widevine.ParsePrivateKey(pemBytes)
   if err != nil {
      return nil, err
   }
   signedBytes, err := widevine.BuildSignedMessage(req_bytes, privateKey)
   if err != nil {
      return nil, err
   }

   respBytes, err := c.Send(signedBytes)
   if err != nil {
      return nil, err
   }

   keys, err := widevine.ParseLicenseResponse(respBytes, req_bytes, privateKey)
   if err != nil {
      return nil, err
   }

   foundKey, ok := widevine.GetKey(keys, media.key_id)
   if !ok {
      return nil, fmt.Errorf("GetKey: key not found in response")
   }

   var zero [16]byte
   if bytes.Equal(foundKey, zero[:]) {
      return nil, fmt.Errorf("zero key received")
   }

   log.Printf("key %x", foundKey)
   return foundKey, nil
}

func (c *Config) download(group []*dash.Representation) error {
   rep := group[0]
   var media MediaFile

   // Configure PSSH if available in MPD
   if err := media.configureProtection(rep); err != nil {
      return err
   }

   file, err := createOutputFile(rep)
   if err != nil {
      return err
   }
   defer file.Close()

   if err := c.downloadInitialization(file, &media, rep); err != nil {
      return err
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
      go func() {
         defer wg.Done()
         for downloadJob := range jobs {
            data, err := getSegment(downloadJob.request.url, downloadJob.request.header)
            results <- result{index: downloadJob.index, data: data, err: err}
         }
      }()
   }

   // Start Writer (processes results)
   doneChan := make(chan error, 1)
   go media.processAndWriteSegments(doneChan, results, len(requests), key, file)

   // Send Jobs
   for reqIndex, req := range requests {
      jobs <- job{index: reqIndex, request: req}
   }
   close(jobs)

   // Wait for writer to finish
   return <-doneChan
}

func (c *Config) downloadInitialization(file *os.File, media *MediaFile, rep *dash.Representation) error {
   var targetURL *url.URL
   var head http.Header
   var err error

   // 1. Resolve the Initialization URL and Headers based on the manifest type
   if rep.SegmentBase != nil {
      head = make(http.Header)
      head.Set("Range", "bytes="+rep.SegmentBase.Initialization.Range)
      targetURL, err = rep.ResolveBaseURL()
   } else if tmpl := rep.GetSegmentTemplate(); tmpl != nil && tmpl.Initialization != "" {
      targetURL, err = tmpl.ResolveInitialization(rep)
   } else if rep.SegmentList != nil {
      targetURL, err = rep.SegmentList.Initialization.ResolveSourceURL()
   }

   // 2. Handle errors or early exit if no init segment exists
   if err != nil {
      return err
   }
   if targetURL == nil {
      return nil
   }

   // 3. Download, Process, and Write
   data, err := getSegment(targetURL, head)
   if err != nil {
      return err
   }

   data, err = media.processInit(data)
   if err != nil {
      return err
   }

   _, err = file.Write(data)
   return err
}

func (c *Config) fetchKey(media *MediaFile) ([]byte, error) {
   if media.key_id == nil {
      return nil, nil
   }
   if c.CertificateChain != "" {
      if c.EncryptSignKey != "" {
         return c.playReadyKey(media)
      }
   }
   return c.widevineKey(media)
}

func (c *Config) playReadyKey(media *MediaFile) ([]byte, error) {
   chainData, err := os.ReadFile(c.CertificateChain)
   if err != nil {
      return nil, err
   }
   var chain playReady.Chain
   if err := chain.Decode(chainData); err != nil {
      return nil, err
   }

   signKeyData, err := os.ReadFile(c.EncryptSignKey)
   if err != nil {
      return nil, err
   }
   encryptSignKey := new(big.Int).SetBytes(signKeyData)

   log.Printf("key ID %x", media.key_id)
   playReady.UuidOrGuid(media.key_id)

   body, err := chain.RequestBody(media.key_id, encryptSignKey)
   if err != nil {
      return nil, err
   }

   respData, err := c.Send(body)
   if err != nil {
      return nil, err
   }

   var license playReady.License
   coord, err := license.Decrypt(respData, encryptSignKey)
   if err != nil {
      return nil, err
   }

   if !bytes.Equal(license.ContentKey.KeyId[:], media.key_id) {
      return nil, ErrKeyMismatch
   }

   key := coord.Key()
   log.Printf("key %x", key)
   return key, nil
}

// Config holds downloader configuration
type Config struct {
   Send             func([]byte) ([]byte, error)
   Threads          int
   CertificateChain string
   EncryptSignKey   string
   ClientId         string
   PrivateKey       string
}

func getMediaRequests(group []*dash.Representation) ([]mediaRequest, error) {
   rep := group[0]
   baseURL, err := rep.ResolveBaseURL()
   if err != nil {
      return nil, err
   }

   template := rep.GetSegmentTemplate()
   switch {
   case template != nil:
      var requests []mediaRequest
      for _, variant := range group {
         addrs, err := template.GetSegmentURLs(variant)
         if err != nil {
            return nil, err
         }
         for _, addr := range addrs {
            requests = append(requests, mediaRequest{url: addr})
         }
      }
      return requests, nil

   case rep.SegmentList != nil:
      var requests []mediaRequest
      for _, seg := range rep.SegmentList.SegmentURLs {
         addr, err := seg.ResolveMedia()
         if err != nil {
            return nil, err
         }
         requests = append(requests, mediaRequest{url: addr})
      }
      return requests, nil

   case rep.SegmentBase != nil:
      head := http.Header{}
      head.Set("Range", "bytes="+rep.SegmentBase.IndexRange)
      data, err := getSegment(baseURL, head)
      if err != nil {
         return nil, err
      }
      parsed, err := sofia.Parse(data)
      if err != nil {
         return nil, err
      }

      var idx indexRange
      if err = idx.Set(rep.SegmentBase.IndexRange); err != nil {
         return nil, err
      }

      sidx, ok := sofia.FindSidx(parsed)
      if !ok {
         return nil, sofia.Missing("sidx")
      }

      requests := make([]mediaRequest, len(sidx.References))
      for refIndex, ref := range sidx.References {
         idx[0] = idx[1] + 1
         idx[1] += uint64(ref.ReferencedSize)

         rangeHead := http.Header{}
         rangeHead.Set("Range", "bytes="+idx.String())
         requests[refIndex] = mediaRequest{url: baseURL, header: rangeHead}
      }
      return requests, nil
   }
   return []mediaRequest{{url: baseURL}}, nil
}

func getSegment(targetURL *url.URL, head http.Header) ([]byte, error) {
   req, err := http.NewRequest("GET", targetURL.String(), nil)
   if err != nil {
      return nil, err
   }
   if head != nil {
      req.Header = head
   }

   resp, err := http.DefaultClient.Do(req)
   if err != nil {
      return nil, err
   }
   defer resp.Body.Close()

   if resp.StatusCode != http.StatusOK {
      if resp.StatusCode != http.StatusPartialContent {
         var msg strings.Builder
         io.Copy(&msg, resp.Body)
         return nil, fmt.Errorf("status %s: %s", resp.Status, msg.String())
      }
   }
   return io.ReadAll(resp.Body)
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

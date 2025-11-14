package net

import (
   "41.neocities.org/dash"
   "41.neocities.org/drm/playReady"
   "41.neocities.org/drm/widevine"
   "41.neocities.org/sofia"
   "bytes"
   "errors"
   "log"
   "math/big"
   "net/http"
   "os"
   "slices"
)

func (c *Config) widevine_key(media *media_file) ([]byte, error) {
   if media.key_id == nil {
      return nil, nil
   }
   private_key, err := os.ReadFile(c.PrivateKey)
   if err != nil {
      return nil, err
   }
   client_id, err := os.ReadFile(c.ClientId)
   if err != nil {
      return nil, err
   }
   var cdm widevine.Cdm
   err = cdm.New(private_key, client_id, media.pssh)
   if err != nil {
      return nil, err
   }
   data, err := cdm.RequestBody()
   if err != nil {
      return nil, err
   }
   data, err = c.Send(data)
   if err != nil {
      return nil, err
   }
   var body widevine.ResponseBody
   err = body.Unmarshal(data)
   if err != nil {
      return nil, err
   }
   block, err := cdm.Block(body)
   if err != nil {
      return nil, err
   }
   for container := range body.Container() {
      if bytes.Equal(container.Id(), media.key_id) {
         key, err := container.Key(block)
         if err != nil {
            return nil, err
         }
         log.Printf("key %x", key)
         var zero [16]byte
         if !bytes.Equal(key, zero[:]) {
            return key, nil
         }
      }
   }
   return nil, errors.New("widevine_key")
}

func (c *Config) playReady_key(media *media_file) ([]byte, error) {
   data, err := os.ReadFile(c.CertificateChain)
   if err != nil {
      return nil, err
   }
   var chain playReady.Chain
   err = chain.Decode(data)
   if err != nil {
      return nil, err
   }
   data, err = os.ReadFile(c.EncryptSignKey)
   if err != nil {
      return nil, err
   }
   encryptSignKey := new(big.Int).SetBytes(data)
   log.Printf("key ID %x", media.key_id)
   playReady.UuidOrGuid(media.key_id)
   data, err = chain.RequestBody(media.key_id, encryptSignKey)
   if err != nil {
      return nil, err
   }
   data, err = c.Send(data)
   if err != nil {
      return nil, err
   }
   var license playReady.License
   coord, err := license.Decrypt(data, encryptSignKey)
   if err != nil {
      return nil, err
   }
   if !bytes.Equal(license.ContentKey.KeyId[:], media.key_id) {
      return nil, errors.New("key ID mismatch")
   }
   key := coord.Key()
   log.Printf("key %x", key)
   return key, nil
}

func (c *Config) key(media *media_file) ([]byte, error) {
   if c.CertificateChain != "" {
      if c.EncryptSignKey != "" {
         return c.playReady_key(media)
      }
   }
   return c.widevine_key(media)
}

func (c *Config) download_initialization(
   represent *dash.Representation, media *media_file, fileVar *os.File,
) error {
   var (
      data []byte
      err  error
   )
   switch {
   case represent.SegmentList != nil:
      data, err = get_segment(represent.SegmentList.Initialization.SourceUrl[0], nil)

   case represent.SegmentTemplate != nil && represent.SegmentTemplate.Initialization != "":
      address, urlErr := represent.SegmentTemplate.Initialization.Url(represent)
      if urlErr != nil {
         return urlErr
      }
      data, err = get_segment(address, nil)

   case represent.SegmentBase != nil:
      head := http.Header{}
      head.Set("range", "bytes="+represent.SegmentBase.Initialization.Range)
      data, err = get_segment(represent.BaseUrl[0], head)

   default:
      // No initialization segment to download
      return nil
   }

   if err != nil {
      return err
   }
   data, err = media.initialization(data)
   if err != nil {
      return err
   }
   _, err = fileVar.Write(data)
   return err
}

type Config struct {
   Send func([]byte) ([]byte, error)
   // Number of segments to download in parallel
   Threads int
   // PlayReady
   CertificateChain string
   EncryptSignKey   string
   // Widevine
   ClientId   string
   PrivateKey string
}

func (c *Config) Download(represent *dash.Representation) error {
   var media media_file
   if err := media.New(represent); err != nil {
      return err
   }
   fileVar, err := create(represent)
   if err != nil {
      return err
   }
   defer fileVar.Close()

   if err := c.download_initialization(represent, &media, fileVar); err != nil {
      return err
   }

   key, err := c.key(&media)
   if err != nil {
      return err
   }

   requests, err := c.get_media_requests(represent)
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
   doneChan := make(chan error, 1)

   // Launch the writer goroutine as a method on our media_file instance.
   // This is much cleaner than the previous closure.
   go media.processAndWriteSegments(doneChan, results, len(requests), key, fileVar)

   // Start worker goroutines
   for w := 0; w < numWorkers; w++ {
      go func() {
         for j := range jobs {
            data, err := get_segment(j.request.url, j.request.header)
            results <- result{index: j.index, data: data, err: err}
         }
      }()
   }

   // Send all jobs
   for i, req := range requests {
      jobs <- job{index: i, request: req}
   }
   close(jobs)

   // Block and wait for the final status from the writer.
   return <-doneChan
}

func (c *Config) get_media_requests(represent *dash.Representation) ([]media_request, error) {
   switch {
   case represent.SegmentTemplate != nil:
      var segments []int
      for rep := range represent.Representation() {
         segments = slices.AppendSeq(segments, rep.Segment())
      }
      requests := make([]media_request, len(segments))
      for i, segment := range segments {
         address, err := represent.SegmentTemplate.Media.Url(represent, segment)
         if err != nil {
            return nil, err
         }
         requests[i] = media_request{url: address}
      }
      return requests, nil
   case represent.SegmentList != nil:
      requests := make([]media_request, len(represent.SegmentList.SegmentUrl))
      for i, segment := range represent.SegmentList.SegmentUrl {
         requests[i] = media_request{url: segment.Media[0]}
      }
      return requests, nil
   case represent.SegmentBase != nil:
      head := http.Header{}
      head.Set("range", "bytes="+represent.SegmentBase.IndexRange)
      data, err := get_segment(represent.BaseUrl[0], head)
      if err != nil {
         return nil, err
      }
      parsed, err := sofia.Parse(data)
      if err != nil {
         return nil, err
      }
      var index index_range
      if err = index.Set(represent.SegmentBase.IndexRange); err != nil {
         return nil, err
      }
      sidx, ok := sofia.FindSidx(parsed)
      if !ok {
         return nil, sofia.Missing("sidx")
      }
      requests := make([]media_request, len(sidx.References))
      for i, reference := range sidx.References {
         index[0] = index[1] + 1
         index[1] += uint64(reference.ReferencedSize)
         range_head := http.Header{}
         range_head.Set("range", "bytes="+index.String())
         requests[i] = media_request{
            url: represent.BaseUrl[0], header: range_head,
         }
      }
      return requests, nil
   }
   return []media_request{
      {url: represent.BaseUrl[0]},
   }, nil
}

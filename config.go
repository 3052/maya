package net

import (
   "41.neocities.org/dash"
   "41.neocities.org/drm/playReady"
   "41.neocities.org/drm/widevine"
   "41.neocities.org/sofia/file"
   "bytes"
   "encoding/base64"
   "errors"
   "log"
   "math/big"
   "net/http"
   "os"
   "slices"
)

type Sender func([]byte) ([]byte, error)

type WidevineConfig struct {
   ClientId   string
   PrivateKey string
   Send Sender
}

type PlayReadyConfig struct {
   CertificateChain string
   Send Sender
   EncryptSignKey string
}

func (w *WidevineConfig) playReady_key(media *media_file) ([]byte, error) {
   home, err := os.UserHomeDir()
   if err != nil {
      return nil, err
   }
   data, err := os.ReadFile(home + "/.cache/certificate")
   if err != nil {
      return nil, err
   }
   var chain playReady.Chain
   err = chain.Decode(data)
   if err != nil {
      return nil, err
   }
   data, err = os.ReadFile(home + "/.cache/signEncryptKey")
   if err != nil {
      return nil, err
   }
   signEncryptKey := new(big.Int).SetBytes(data)
   log.Printf("key ID %x", media.key_id)
   playReady.UuidOrGuid(media.key_id)
   data, err = chain.RequestBody(media.key_id, signEncryptKey)
   if err != nil {
      return nil, err
   }
   data, err = w.Send(data)
   if err != nil {
      return nil, err
   }
   var license playReady.License
   coord, err := license.Decrypt(data, signEncryptKey)
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

func (w *WidevineConfig) segment_base(represent *dash.Representation) error {
   if Threads != 1 {
      return errors.New("Threads")
   }
   var media media_file
   err := media.New(represent)
   if err != nil {
      return err
   }
   os_file, err := create(represent)
   if err != nil {
      return err
   }
   defer os_file.Close()
   head := http.Header{}
   head.Set("range", "bytes="+represent.SegmentBase.Initialization.Range)
   data, err := get_segment(represent.BaseUrl[0], head)
   if err != nil {
      return err
   }
   data, err = media.initialization(data)
   if err != nil {
      return err
   }
   _, err = os_file.Write(data)
   if err != nil {
      return err
   }
   var widevine_key bool
   if w.ClientId != "" {
      if w.PrivateKey != "" {
         widevine_key = true
      }
   }
   var key []byte
   if widevine_key {
      key, err = w.widevine_key(&media)
   } else {
      key, err = w.playReady_key(&media)
   }
   if err != nil {
      return err
   }
   head.Set("range", "bytes="+represent.SegmentBase.IndexRange)
   data, err = get_segment(represent.BaseUrl[0], head)
   if err != nil {
      return err
   }
   var file_file file.File
   err = file_file.Read(data)
   if err != nil {
      return err
   }
   var progressVar progress
   progressVar.set(len(file_file.Sidx.Reference))
   var index index_range
   err = index.Set(represent.SegmentBase.IndexRange)
   if err != nil {
      return err
   }
   for _, reference := range file_file.Sidx.Reference {
      index[0] = index[1] + 1
      index[1] += uint64(reference.Size())
      head.Set("range", "bytes="+index.String())
      data, err = get_segment(represent.BaseUrl[0], head)
      if err != nil {
         return err
      }
      progressVar.next()
      data, err = media.write_segment(data, key)
      if err != nil {
         return err
      }
      _, err = os_file.Write(data)
      if err != nil {
         return err
      }
   }
   return nil
}

func (w *WidevineConfig) widevine_key(media *media_file) ([]byte, error) {
   if media.key_id == nil {
      return nil, nil
   }
   private_key, err := os.ReadFile(w.PrivateKey)
   if err != nil {
      return nil, err
   }
   client_id, err := os.ReadFile(w.ClientId)
   if err != nil {
      return nil, err
   }
   if media.pssh == nil {
      var psshVar widevine.Pssh
      psshVar.KeyIds = [][]byte{media.key_id}
      media.pssh = psshVar.Marshal()
   }
   log.Println("PSSH", base64.StdEncoding.EncodeToString(media.pssh))
   var cdm widevine.Cdm
   err = cdm.New(private_key, client_id, media.pssh)
   if err != nil {
      return nil, err
   }
   data, err := cdm.RequestBody()
   if err != nil {
      return nil, err
   }
   data, err = w.Send(data)
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
         key := container.Key(block)
         log.Printf("key %x", key)
         var zero [16]byte
         if !bytes.Equal(key, zero[:]) {
            return key, nil
         }
      }
   }
   return nil, errors.New("widevine_key")
}

func (w *WidevineConfig) segment_template(represent *dash.Representation) error {
   var media media_file
   err := media.New(represent)
   if err != nil {
      return err
   }
   fileVar, err := create(represent)
   if err != nil {
      return err
   }
   defer fileVar.Close()
   if initial := represent.SegmentTemplate.Initialization; initial != "" {
      address, err := initial.Url(represent)
      if err != nil {
         return err
      }
      data1, err := get_segment(address, nil)
      if err != nil {
         return err
      }
      data1, err = media.initialization(data1)
      if err != nil {
         return err
      }
      _, err = fileVar.Write(data1)
      if err != nil {
         return err
      }
   }
   key, err := w.widevine_key(&media)
   if err != nil {
      return err
   }
   var segments []int
   for rep := range represent.Representation() {
      segments = slices.AppendSeq(segments, rep.Segment())
   }
   var progressVar progress
   progressVar.set(len(segments))
   for chunk := range slices.Chunk(segments, Threads) {
      var (
         datas = make([][]byte, len(chunk))
         errs  = make(chan error)
      )
      for i, segment := range chunk {
         address, err := represent.SegmentTemplate.Media.Url(represent, segment)
         if err != nil {
            return err
         }
         go func() {
            datas[i], err = get_segment(address, nil)
            errs <- err
            progressVar.next()
         }()
      }
      for range chunk {
         err := <-errs
         if err != nil {
            return err
         }
      }
      for _, data := range datas {
         data, err = media.write_segment(data, key)
         if err != nil {
            return err
         }
         _, err = fileVar.Write(data)
         if err != nil {
            return err
         }
      }
   }
   return nil
}

func (w *WidevineConfig) segment_list(represent *dash.Representation) error {
   if Threads != 1 {
      return errors.New("Threads")
   }
   var media media_file
   err := media.New(represent)
   if err != nil {
      return err
   }
   fileVar, err := create(represent)
   if err != nil {
      return err
   }
   defer fileVar.Close()
   data, err := get_segment(
      represent.SegmentList.Initialization.SourceUrl[0], nil,
   )
   if err != nil {
      return err
   }
   data, err = media.initialization(data)
   if err != nil {
      return err
   }
   _, err = fileVar.Write(data)
   if err != nil {
      return err
   }
   key, err := w.widevine_key(&media)
   if err != nil {
      return err
   }
   var progressVar progress
   progressVar.set(len(represent.SegmentList.SegmentUrl))
   for _, segment := range represent.SegmentList.SegmentUrl {
      data, err := get_segment(segment.Media[0], nil)
      if err != nil {
         return err
      }
      progressVar.next()
      data, err = media.write_segment(data, key)
      if err != nil {
         return err
      }
      _, err = fileVar.Write(data)
      if err != nil {
         return err
      }
   }
   return nil
}

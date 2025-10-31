package net

import (
   "41.neocities.org/dash"
   "41.neocities.org/drm/widevine"
   "41.neocities.org/sofia"
   "bytes"
   "encoding/base64"
   "errors"
   "log"
   "os"
)

func (m *media_file) initialization(data []byte) ([]byte, error) {
   parsedInit, err := sofia.Parse(data)
   if err != nil {
      return nil, err
   }
   moov, ok := sofia.FindMoov(parsedInit)
   if !ok {
      return nil, errors.New("could not find 'moov' box in init file")
   }
   trak, ok := moov.Trak()
   if !ok {
      return nil, errors.New("could not find 'trak' in moov")
   }
   // THIS FIXES A/V SYNC WITH
   // rakuten.tv
   // BUT MIGHT BREAK THESE
   // criterionchannel.com
   // mubi.com
   // paramountplus.com
   // tubitv.com
   trak.ReplaceEdts()
   mdhd, ok := trak.Mdhd()
   if !ok {
      return nil, errors.New("could not find 'mdhd' in trak to get timescale")
   }
   m.timescale = uint64(mdhd.Timescale)
   err = moov.Sanitize()
   if err != nil {
      return nil, err
   }
   m.key_id = trak.Tenc().DefaultKID[:]
   if m.pssh == nil {
      widevine_box, _ := moov.FindPssh(widevine_id)
      m.pssh = widevine_box.Data
      log.Println("MP4 PSSH", base64.StdEncoding.EncodeToString(m.pssh))
   }
   var finalMP4Data bytes.Buffer
   for _, box := range parsedInit {
      finalMP4Data.Write(box.Encode())
   }
   return finalMP4Data.Bytes(), nil
}

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
const widevine_urn = "urn:uuid:edef8ba9-79d6-4ace-a3c8-27dcd51d21ed"

func (m *media_file) New(represent *dash.Representation) error {
   for _, content := range represent.ContentProtection {
      if content.SchemeIdUri == widevine_urn {
         if content.Pssh != "" {
            data, err := base64.StdEncoding.DecodeString(content.Pssh)
            if err != nil {
               return err
            }
            var box sofia.PsshBox
            err = box.Parse(data)
            if err != nil {
               return err
            }
            m.pssh = box.Data
            log.Println("MPD PSSH", base64.StdEncoding.EncodeToString(m.pssh))
            break
         }
      }
   }
   return nil
}

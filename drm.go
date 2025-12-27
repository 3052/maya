package maya

import (
   "41.neocities.org/drm/playReady"
   "41.neocities.org/drm/widevine"
   "41.neocities.org/sofia"
   "bytes"
   "errors"
   "log"
   "math/big"
   "os"
)

const (
   // widevineSystemId is the UUID for the Widevine DRM system.
   widevineSystemId = "edef8ba979d64acea3c827dcd51d21ed"
)

var (
   errKeyMismatch = errors.New("key ID mismatch")
)

type mediaFile struct {
   key_id     []byte
   content_id []byte
}

// ingestWidevinePssh parses Widevine PSSH data and sets the ContentId.
// It assumes the caller has determined this parsing is necessary.
func (m *mediaFile) ingestWidevinePssh(data []byte) error {
   var pssh_data widevine.PsshData
   if err := pssh_data.Unmarshal(data); err != nil {
      return err
   }
   if pssh_data.ContentId != nil {
      m.content_id = pssh_data.ContentId
      log.Printf("content ID %x", m.content_id)
   }
   return nil
}

func (m *mediaFile) configureProtection(protections []protectionInfo) error {
   for _, protect := range protections {
      switch protect.Scheme {
      case "widevine":
         if len(protect.Pssh) > 0 {
            var pssh_box sofia.PsshBox
            if err := pssh_box.Parse(protect.Pssh); err == nil {
               if err := m.ingestWidevinePssh(pssh_box.Data); err != nil {
                  return err
               }
            }
         }
         if len(protect.KeyID) > 0 {
            m.key_id = protect.KeyID
            log.Printf("key ID %x", m.key_id)
         }
      case "playready":
         // TODO: Implement PlayReady configuration
      }
   }
   return nil
}

func (c *Config) fetchKey(media *mediaFile) ([]byte, error) {
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

func (c *Config) widevineKey(media *mediaFile) ([]byte, error) {
   client_id, err := os.ReadFile(c.ClientId)
   if err != nil {
      return nil, err
   }
   pemBytes, err := os.ReadFile(c.PrivateKey)
   if err != nil {
      return nil, err
   }
   var pssh widevine.PsshData
   pssh.ContentId = media.content_id
   pssh.KeyIds = [][]byte{media.key_id}
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
      return nil, errors.New("GetKey: key not found in response")
   }
   var zero [16]byte
   if bytes.Equal(foundKey, zero[:]) {
      return nil, errors.New("zero key received")
   }
   log.Printf("key %x", foundKey)
   return foundKey, nil
}

func (c *Config) playReadyKey(media *mediaFile) ([]byte, error) {
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
      return nil, errKeyMismatch
   }
   key := coord.Key()
   log.Printf("key %x", key)
   return key, nil
}

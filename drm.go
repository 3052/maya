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

// protectionInfo holds standardized DRM data extracted from a manifest.
type protectionInfo struct {
   Pssh  []byte
   KeyID []byte
}

type mediaFile struct {
   key_id     []byte
   content_id []byte
}

// drmConfig holds the credentials for a single download action. The DRM system
// is inferred from which fields are populated.
type drmConfig struct {
   CertificateChain string
   EncryptSignKey   string
   ClientId         string
   PrivateKey       string
}

// ingestWidevinePssh parses Widevine PSSH data and sets the ContentId and KeyID.
func (m *mediaFile) ingestWidevinePssh(data []byte) error {
   var pssh_data widevine.PsshData
   if err := pssh_data.Unmarshal(data); err != nil {
      return err
   }
   if pssh_data.ContentId != nil {
      m.content_id = pssh_data.ContentId
      log.Printf("content ID %x", m.content_id)
   }
   // If Key ID isn't set yet (e.g. from manifest) and PSSH has one, use it.
   if m.key_id == nil && len(pssh_data.KeyIds) > 0 {
      m.key_id = pssh_data.KeyIds[0]
      log.Printf("key ID from PSSH: %x", m.key_id)
   }
   return nil
}

// configureProtection copies the necessary data from the manifest into the mediaFile.
func (m *mediaFile) configureProtection(protection *protectionInfo) error {
   if len(protection.Pssh) > 0 {
      var pssh_box sofia.PsshBox
      if err := pssh_box.Parse(protection.Pssh); err == nil {
         if err := m.ingestWidevinePssh(pssh_box.Data); err != nil {
            return err
         }
      }
   }
   if len(protection.KeyID) > 0 {
      m.key_id = protection.KeyID
      log.Printf("key ID %x", m.key_id)
   }
   return nil
}

// fetchKey dispatches to the correct DRM key function. It is a method on Config to access the Send func.
func (c *Config) fetchKey(drmCfg *drmConfig, media *mediaFile) ([]byte, error) {
   if drmCfg == nil || media.key_id == nil {
      return nil, nil // No DRM or unencrypted.
   }

   // Infer DRM scheme from which credentials are set.
   isWidevine := drmCfg.ClientId != "" && drmCfg.PrivateKey != ""
   isPlayReady := drmCfg.CertificateChain != "" && drmCfg.EncryptSignKey != ""

   if isWidevine {
      return c.widevineKey(drmCfg, media)
   } else if isPlayReady {
      return c.playReadyKey(drmCfg, media)
   }

   return nil, nil // No valid DRM config found
}

func (c *Config) widevineKey(drmCfg *drmConfig, media *mediaFile) ([]byte, error) {
   if drmCfg.ClientId == "" || drmCfg.PrivateKey == "" {
      return nil, errors.New("widevine requires ClientId and PrivateKey paths")
   }
   client_id, err := os.ReadFile(drmCfg.ClientId)
   if err != nil {
      return nil, err
   }
   pemBytes, err := os.ReadFile(drmCfg.PrivateKey)
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

func (c *Config) playReadyKey(drmCfg *drmConfig, media *mediaFile) ([]byte, error) {
   if drmCfg.CertificateChain == "" || drmCfg.EncryptSignKey == "" {
      return nil, errors.New("playready requires CertificateChain and EncryptSignKey paths")
   }
   chainData, err := os.ReadFile(drmCfg.CertificateChain)
   if err != nil {
      return nil, err
   }
   var chain playReady.Chain
   if err := chain.Decode(chainData); err != nil {
      return nil, err
   }
   signKeyData, err := os.ReadFile(drmCfg.EncryptSignKey)
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

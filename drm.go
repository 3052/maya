package maya

import (
   "41.neocities.org/drm/playReady"
   "41.neocities.org/drm/widevine"
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

// protectionInfo holds standardized DRM data extracted from a manifest or init segment.
type protectionInfo struct {
   Pssh  []byte
   KeyID []byte
}

// WidevineConfig holds the specific credentials for Widevine license requests.
type WidevineConfig struct {
   ClientID   string
   PrivateKey string
}

// PlayReadyConfig holds the specific credentials for PlayReady license requests.
type PlayReadyConfig struct {
   CertificateChain string
   EncryptSignKey   string
}

// fetchKey dispatches to the correct DRM key function based on the provided configuration.
func (c *Config) fetchKey(keyID []byte, contentID []byte) ([]byte, error) {
   if (c.Widevine == nil && c.PlayReady == nil) || keyID == nil {
      return nil, nil // No DRM config or no Key ID found.
   }

   if c.Widevine != nil {
      return c.widevineKey(c.Widevine, keyID, contentID)
   }
   if c.PlayReady != nil {
      return c.playReadyKey(c.PlayReady, keyID)
   }

   return nil, nil // No valid DRM config found
}

func (c *Config) widevineKey(wvCfg *WidevineConfig, keyID []byte, contentID []byte) ([]byte, error) {
   if wvCfg.ClientID == "" || wvCfg.PrivateKey == "" {
      return nil, errors.New("widevine requires ClientID and PrivateKey paths")
   }
   client_id, err := os.ReadFile(wvCfg.ClientID)
   if err != nil {
      return nil, err
   }
   pemBytes, err := os.ReadFile(wvCfg.PrivateKey)
   if err != nil {
      return nil, err
   }
   var pssh widevine.PsshData
   pssh.ContentId = contentID
   pssh.KeyIds = [][]byte{keyID}
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
   foundKey, ok := widevine.GetKey(keys, keyID)
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

func (c *Config) playReadyKey(prCfg *PlayReadyConfig, keyID []byte) ([]byte, error) {
   if prCfg.CertificateChain == "" || prCfg.EncryptSignKey == "" {
      return nil, errors.New("playready requires CertificateChain and EncryptSignKey paths")
   }
   chainData, err := os.ReadFile(prCfg.CertificateChain)
   if err != nil {
      return nil, err
   }
   var chain playReady.Chain
   if err := chain.Decode(chainData); err != nil {
      return nil, err
   }
   signKeyData, err := os.ReadFile(prCfg.EncryptSignKey)
   if err != nil {
      return nil, err
   }
   encryptSignKey := new(big.Int).SetBytes(signKeyData)
   playReady.UuidOrGuid(keyID)
   body, err := chain.RequestBody(keyID, encryptSignKey)
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
   if !bytes.Equal(license.ContentKey.KeyId[:], keyID) {
      return nil, errKeyMismatch
   }
   key := coord.Key()
   log.Printf("key %x", key)
   return key, nil
}

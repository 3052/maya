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

func (j *WidevineJob) widevineKey(keyID []byte, contentID []byte) ([]byte, error) {
   if j.ClientID == "" || j.PrivateKey == "" {
      return nil, errors.New("widevine requires ClientID and PrivateKey paths")
   }
   client_id, err := os.ReadFile(j.ClientID)
   if err != nil {
      return nil, err
   }
   pemBytes, err := os.ReadFile(j.PrivateKey)
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
   respBytes, err := j.Send(signedBytes)
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

func (j *PlayReadyJob) playReadyKey(keyID []byte) ([]byte, error) {
   if j.CertificateChain == "" || j.EncryptSignKey == "" {
      return nil, errors.New("playready requires CertificateChain and EncryptSignKey paths")
   }
   chainData, err := os.ReadFile(j.CertificateChain)
   if err != nil {
      return nil, err
   }
   var chain playReady.Chain
   if err := chain.Decode(chainData); err != nil {
      return nil, err
   }
   signKeyData, err := os.ReadFile(j.EncryptSignKey)
   if err != nil {
      return nil, err
   }
   encryptSignKey := new(big.Int).SetBytes(signKeyData)
   playReady.UuidOrGuid(keyID)
   body, err := chain.RequestBody(keyID, encryptSignKey)
   if err != nil {
      return nil, err
   }
   respData, err := j.Send(body)
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

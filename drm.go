package maya

import (
   "41.neocities.org/drm/playReady"
   "41.neocities.org/drm/widevine"
   "bytes"
   "errors"
   "fmt"
   "log"
   "math/big"
   "os"
)

func getKeyForStream(fetcher keyFetcher, manifestProtection, initProtection *protectionInfo) ([]byte, error) {
   var keyId, contentId []byte
   // Priority for Content ID is: Manifest -> Init Segment
   if manifestProtection != nil && len(manifestProtection.ContentId) > 0 {
      contentId = manifestProtection.ContentId
      log.Printf("content ID from manifest: %s", contentId)
   } else if initProtection != nil && len(initProtection.ContentId) > 0 {
      contentId = initProtection.ContentId
      log.Printf("content ID from MP4: %s", contentId)
   }
   // Key ID MUST come from the init segment ('tenc' box).
   if initProtection != nil && initProtection.KeyId != nil {
      keyId = initProtection.KeyId
      log.Printf("key ID from MP4 tenc: %x", keyId)
   }
   if keyId == nil {
      // If no Key ID is found, we assume the stream is not encrypted and
      // return nil for the key, which will cause the downloader to skip decryption.
      log.Println("No key ID found in MP4 'tenc' box; assuming stream is not encrypted.")
      return nil, nil
   }
   // Finally, fetch the key.
   key, err := fetcher(keyId, contentId)
   if err != nil {
      return nil, fmt.Errorf("failed to fetch decryption key: %w", err)
   }
   return key, nil
}

// widevineSystemId is the UUID for the Widevine DRM system.
const widevineSystemId = "edef8ba979d64acea3c827dcd51d21ed"

var errKeyMismatch = errors.New("key ID mismatch")

// protectionInfo holds standardized DRM data extracted from a manifest or init segment.
type protectionInfo struct {
   ContentId []byte
   KeyId     []byte
}

// keyFetcher is a function type that abstracts the DRM-specific key retrieval process.
type keyFetcher func(keyId, contentId []byte) ([]byte, error)

func (j *WidevineJob) widevineKey(keyId []byte, contentId []byte) ([]byte, error) {
   if j.Send == nil {
      return nil, errors.New("WidevineJob.Send function is not set")
   }
   if j.ClientId == "" || j.PrivateKey == "" {
      return nil, errors.New("widevine requires ClientId and PrivateKey paths")
   }
   client_id, err := os.ReadFile(j.ClientId)
   if err != nil {
      return nil, err
   }
   pemBytes, err := os.ReadFile(j.PrivateKey)
   if err != nil {
      return nil, err
   }
   var pssh widevine.PsshData
   pssh.ContentId = contentId
   pssh.KeyIds = [][]byte{keyId}
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
   foundKey, err := widevine.GetKey(keys, keyId)
   if err != nil {
      return nil, err
   }
   var zero [16]byte
   if bytes.Equal(foundKey, zero[:]) {
      return nil, errors.New("zero key received")
   }
   log.Printf("key %x", foundKey)
   return foundKey, nil
}

func (j *PlayReadyJob) playReadyKey(keyId []byte) ([]byte, error) {
   if j.Send == nil {
      return nil, errors.New("PlayReadyJob.Send function is not set")
   }
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
   playReady.UuidOrGuid(keyId)
   body, err := chain.RequestBody(keyId, encryptSignKey)
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
   if !bytes.Equal(license.ContentKey.KeyId[:], keyId) {
      return nil, errKeyMismatch
   }
   key := coord.Key()
   log.Printf("key %x", key)
   return key, nil
}

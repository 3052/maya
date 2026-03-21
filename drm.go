// drm.go
package maya

import (
   "41.neocities.org/drm/playReady"
   "41.neocities.org/drm/widevine"
   "41.neocities.org/luna/dash"
   "41.neocities.org/sofia"
   "bytes"
   "errors"
   "fmt"
   "log"
   "os"
   "path/filepath"
   "strings"
)

// widevineSystemId is the UUID for the Widevine DRM system.
const widevineSystemId = "edef8ba979d64acea3c827dcd51d21ed"

// protectionInfo holds standardized DRM data extracted from a manifest or init segment.
type protectionInfo struct {
   ContentId []byte
   KeyId     []byte
}

// keyFetcher is a function type that abstracts the DRM-specific key retrieval process.
type keyFetcher func(keyId, contentId []byte) ([]byte, error)

func playReadyKey(folder string, keyId []byte, send Sender) ([]byte, error) {
   if send == nil {
      return nil, errors.New("send function cannot be nil")
   }
   if folder == "" {
      return nil, errors.New("playready requires a folder path")
   }
   // 1. certificate chain
   data, err := os.ReadFile(filepath.Join(folder, "bdevcert.dat"))
   if err != nil {
      return nil, err
   }
   chain, err := playReady.ParseChain(data)
   if err != nil {
      return nil, err
   }
   // 2. signing key
   data, err = os.ReadFile(filepath.Join(folder, "zprivsig.dat"))
   if err != nil {
      return nil, err
   }
   signingKey, err := playReady.ParseRawPrivateKey(data)
   if err != nil {
      return nil, err
   }
   // 3. encrypt key
   data, err = os.ReadFile(filepath.Join(folder, "zprivencr.dat"))
   if err != nil {
      return nil, err
   }
   encryptKey, err := playReady.ParseRawPrivateKey(data)
   if err != nil {
      return nil, err
   }
   playReady.UuidOrGuid(keyId)
   data, err = chain.LicenseRequestBytes(signingKey, keyId)
   if err != nil {
      return nil, err
   }
   data, err = send(data)
   if err != nil {
      return nil, err
   }
   license, err := playReady.ParseLicense(data)
   if err != nil {
      return nil, err
   }
   ok := bytes.Equal(
      license.ContainerOuter.ContainerKeys.ContentKey.GuidKeyID, keyId,
   )
   if !ok {
      return nil, errors.New("key ID mismatch")
   }
   key, err := license.Decrypt(encryptKey)
   if err != nil {
      return nil, err
   }
   log.Printf("key %x", key)
   return key, nil
}

func getKeyForStream(fetcher keyFetcher, manifestProtection, initProtection *protectionInfo) ([]byte, error) {
   var keyId, contentId []byte
   // Priority for Content ID is: Manifest -> Init Segment
   if manifestProtection != nil && len(manifestProtection.ContentId) > 0 {
      contentId = manifestProtection.ContentId
      log.Printf("content ID from manifest: %x", contentId)
   } else if initProtection != nil && len(initProtection.ContentId) > 0 {
      contentId = initProtection.ContentId
      log.Printf("content ID from MP4: %x", contentId)
   }
   // Key ID MUST come from the init segment ('tenc' box).
   if initProtection != nil && initProtection.KeyId != nil {
      keyId = initProtection.KeyId
      log.Printf("key ID from MP4 tenc: %x", keyId)
   }
   if keyId == nil {
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

func widevineKey(folder string, keyId []byte, contentId []byte, send Sender) ([]byte, error) {
   if send == nil {
      return nil, errors.New("send function cannot be nil")
   }
   if folder == "" {
      return nil, errors.New("widevine requires a folder path")
   }
   client_id, err := os.ReadFile(filepath.Join(folder, "client_id.bin"))
   if err != nil {
      return nil, err
   }
   pemBytes, err := os.ReadFile(filepath.Join(folder, "private_key.pem"))
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
   respBytes, err := send(signedBytes)
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

// getDashProtection extracts Widevine PSSH data from a representation.
func getDashProtection(rep *dash.Representation) (*protectionInfo, error) {
   const widevineURN = "urn:uuid:edef8ba9-79d6-4ace-a3c8-27dcd51d21ed"
   var psshData []byte
   for _, contentProtection := range rep.GetContentProtection() {
      if strings.ToLower(contentProtection.SchemeIdUri) == widevineURN {
         pssh, err := contentProtection.GetPssh()
         if err != nil {
            return nil, fmt.Errorf("could not parse widevine pssh from manifest: %w", err)
         }
         if pssh != nil {
            psshData = pssh
            break // Found it
         }
      }
   }

   if psshData == nil {
      return nil, nil
   }

   var psshBox sofia.PsshBox
   if err := psshBox.Parse(psshData); err != nil {
      return nil, fmt.Errorf("could not parse pssh box from dash manifest: %w", err)
   }

   var wvData widevine.PsshData
   if err := wvData.Unmarshal(psshBox.Data); err != nil {
      // Not a fatal error, might just be a PSSH without a content ID
      return &protectionInfo{ContentId: nil, KeyId: nil}, nil
   }

   // The KeyId field is explicitly set to nil, as it must only come from the MP4.
   return &protectionInfo{ContentId: wvData.ContentId, KeyId: nil}, nil
}

// getFetcher determines the appropriate key retrieval logic based on which DRM folder is present.
func (j *Job) getFetcher(send Sender) (keyFetcher, error) {
   if j.Widevine != "" {
      if send == nil {
         return nil, fmt.Errorf("widevine configuration present but send function is nil")
      }
      return func(keyId, contentId []byte) ([]byte, error) {
         return widevineKey(j.Widevine, keyId, contentId, send)
      }, nil
   }
   if j.PlayReady != "" {
      if send == nil {
         return nil, fmt.Errorf("playready configuration present but send function is nil")
      }
      return func(keyId, contentId []byte) ([]byte, error) {
         return playReadyKey(j.PlayReady, keyId, send)
      }, nil
   }
   // Verify that we don't have a sender without a configuration
   if send != nil {
      return nil, fmt.Errorf("send function provided but no DRM configuration found")
   }
   // No DRM config present; return nil fetcher for clear download.
   return nil, nil
}

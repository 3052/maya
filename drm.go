// drm.go
package maya

import (
   "41.neocities.org/diana/playReady"
   "41.neocities.org/diana/widevine"
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

// getKeyFetcher determines the appropriate key retrieval logic based on the DRM options.
func (optionsData *Options) getKeyFetcher() (keyFetcher, error) {
   if optionsData == nil || optionsData.Drm == DrmNone {
      return nil, nil
   }

   if optionsData.License == nil {
      return nil, errors.New("a License function is required when DRM is specified")
   }

   if optionsData.Device == "" {
      return nil, errors.New("a Device path is required when DRM is specified")
   }

   switch optionsData.Drm {
   case DrmWidevine:
      return func(keyId, contentId []byte) ([]byte, error) {
         return widevineKey(optionsData.Device, keyId, contentId, optionsData.License)
      }, nil
   case DrmPlayReady:
      return func(keyId, contentId []byte) ([]byte, error) {
         return playReadyKey(optionsData.Device, keyId, string(contentId), optionsData.License)
      }, nil
   default:
      return nil, fmt.Errorf("unsupported DRM system: %v", optionsData.Drm)
   }
}

const playReadySystemId = "9a04f07998404286ab92e65be0885f95"

const widevineSystemId = "edef8ba979d64acea3c827dcd51d21ed"

func getKeyForStream(fetcher keyFetcher, manifestProtection, initProtection *protectionInfo) ([]byte, error) {
   var keyId, contentId []byte
   if manifestProtection != nil && len(manifestProtection.ContentId) > 0 {
      contentId = manifestProtection.ContentId
      log.Printf("content ID from manifest: %x", contentId)
   } else if initProtection != nil && len(initProtection.ContentId) > 0 {
      contentId = initProtection.ContentId
      log.Printf("content ID from MP4: %x", contentId)
   }

   if initProtection != nil && initProtection.KeyId != nil {
      keyId = initProtection.KeyId
      log.Printf("key ID from MP4 tenc: %x", keyId)
   }

   if keyId == nil {
      log.Println("no key ID found in MP4 'tenc' box; assuming stream is not encrypted")
      return nil, nil
   }

   key, err := fetcher(keyId, contentId)
   if err != nil {
      return nil, fmt.Errorf("failed to fetch decryption key: %w", err)
   }

   return key, nil
}

func playReadyKey(device string, keyId []byte, contentId string, fetchLicense func([]byte) ([]byte, error)) ([]byte, error) {
   data, err := os.ReadFile(filepath.Join(device, "bdevcert.dat"))
   if err != nil {
      return nil, err
   }

   chain, err := playReady.ParseChain(data)
   if err != nil {
      return nil, err
   }

   data, err = os.ReadFile(filepath.Join(device, "zprivsig.dat"))
   if err != nil {
      return nil, err
   }

   signingKey, err := playReady.ParseRawPrivateKey(data)
   if err != nil {
      return nil, err
   }

   data, err = os.ReadFile(filepath.Join(device, "zprivencr.dat"))
   if err != nil {
      return nil, err
   }

   encryptKey, err := playReady.ParseRawPrivateKey(data)
   if err != nil {
      return nil, err
   }

   playReady.UuidOrGuid(keyId)
   data, err = chain.LicenseRequestBytes(signingKey, keyId, contentId)
   if err != nil {
      return nil, err
   }

   data, err = fetchLicense(data)
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

   log.Printf("key: %x", key)
   return key, nil
}

func widevineKey(device string, keyId, contentId []byte, fetchLicense func([]byte) ([]byte, error)) ([]byte, error) {
   client_id, err := os.ReadFile(filepath.Join(device, "device_client_id_blob"))
   if err != nil {
      return nil, err
   }

   pem_data, err := os.ReadFile(filepath.Join(device, "device_private_key"))
   if err != nil {
      return nil, err
   }

   var pssh widevine.PsshData
   pssh.ContentId = contentId
   pssh.KeyIds = [][]byte{keyId}
   req_data, err := pssh.EncodeLicenseRequest(client_id)
   if err != nil {
      return nil, err
   }

   private_key, err := widevine.DecodePrivateKey(pem_data)
   if err != nil {
      return nil, err
   }

   signed_data, err := widevine.EncodeSignedMessage(req_data, private_key)
   if err != nil {
      return nil, err
   }

   resp_data, err := fetchLicense(signed_data)
   if err != nil {
      return nil, err
   }

   keys, err := widevine.DecodeLicenseResponse(resp_data, req_data, private_key)
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

   log.Printf("key: %x", foundKey)
   return foundKey, nil
}

type keyFetcher func(keyId, contentId []byte) ([]byte, error)

type protectionInfo struct {
   ContentId []byte
   KeyId     []byte
}

// getDashProtection extracts Widevine PSSH data from a representation.
func getDashProtection(rep *dash.Representation) (*protectionInfo, error) {
   const widevineUrn = "urn:uuid:edef8ba9-79d6-4ace-a3c8-27dcd51d21ed"
   var pssh_data []byte
   for _, contentProtection := range rep.GetContentProtection() {
      if strings.ToLower(contentProtection.SchemeIdUri) == widevineUrn {
         pssh, err := contentProtection.GetPssh()
         if err != nil {
            return nil, fmt.Errorf("could not parse widevine pssh from manifest: %w", err)
         }

         if pssh != nil {
            pssh_data = pssh
            break
         }
      }
   }

   if pssh_data == nil {
      return nil, nil
   }

   // If the data is wrapped in a standard MP4 pssh box, extract the payload.
   // Otherwise, assume it's already the raw Widevine protobuf data.
   if len(pssh_data) >= 8 && string(pssh_data[4:8]) == "pssh" {
      pssh_box, err := sofia.DecodePsshBox(pssh_data)
      if err != nil {
         return nil, fmt.Errorf("could not parse pssh box from dash manifest: %w", err)
      }
      pssh_data = pssh_box.Data
   }

   wv_data, err := widevine.DecodePsshData(pssh_data)
   if err != nil {
      return nil, fmt.Errorf("could not decode widevine pssh data: %w", err)
   }

   return &protectionInfo{ContentId: wv_data.ContentId, KeyId: nil}, nil
}

package maya

import (
   "41.neocities.org/luna/dash"
   "41.neocities.org/luna/hls"
   "encoding/xml"
   "errors"
   "fmt"
   "io"
   "log"
   "net/http"
   "net/url"
   "os"
   "path"
   "path/filepath"
   "strings"
)

// Update accepts an optional optionalRead boolean.
// Usage:
// c.Update(val, fn)       -> Read is required. Fails if Read fails.
// c.Update(val, fn, true) -> Read is optional. If Read fails, proceeds with empty value.
func (c *Cache) Update(value any, update func() error, optionalRead ...bool) error {
   // Pass the flag specifically to the Read method
   if err := c.Read(value, optionalRead...); err != nil {
      return err
   }
   // The callback is NOT optional; if it fails, we return the error
   if err := update(); err != nil {
      return err
   }
   // The Write is NOT optional; if it fails, we return the error
   return c.Write(value)
}

type Cache struct {
   path string
}

// Read accepts an optional optionalRead boolean.
func (c *Cache) Read(value any, optionalRead ...bool) error {
   data, err := os.ReadFile(c.path)
   if err != nil {
      // Case 2: Read is optional.
      // If the file cannot be read (for any reason), we suppress the error
      // and return nil so the caller can start with a fresh/empty value.
      if len(optionalRead) > 0 && optionalRead[0] {
         return nil
      }
      // Case 1: Read is required (Default).
      // Return the error to stop execution.
      return err
   }
   return xml.Unmarshal(data, value)
}

func (c *Cache) Write(value any) error {
   data, err := xml.Marshal(value)
   if err != nil {
      return err
   }
   log.Println("Write:", c.path)
   return os.WriteFile(c.path, data, os.ModePerm)
}

// ResolveCache joins the user cache directory with the provided path
func ResolveCache(path string) (string, error) {
   baseDir, err := os.UserCacheDir()
   if err != nil {
      return "", err
   }
   return filepath.Join(baseDir, path), nil
}

// It relies on the struct's state for configuration.
func (c *Cache) Setup(path string) error {
   var err error
   c.path, err = ResolveCache(path)
   if err != nil {
      return err
   }
   // Create the directory immediately
   return os.MkdirAll(filepath.Dir(c.path), os.ModePerm)
}

func SetProxy(proxyUrlStr, excludePatternsStr string) error {
   var parsedProxy *url.URL
   if proxyUrlStr != "" {
      var err error
      parsedProxy, err = url.Parse(proxyUrlStr)
      if err != nil {
         return err
      }
   }
   // Split patterns by comma
   patterns := strings.Split(excludePatternsStr, ",")
   // Assign directly to the global DefaultTransport.
   // We ignore any existing values in the previous DefaultTransport.
   http.DefaultTransport = &http.Transport{
      Proxy: func(req *http.Request) (*url.URL, error) {
         fileName := path.Base(req.URL.Path)
         // Check exclusion patterns
         for _, pattern := range patterns {
            matched, err := path.Match(pattern, fileName)
            if err != nil {
               return nil, err
            }
            if matched {
               // Pattern matched: Bypass proxy, do not log.
               return nil, nil
            }
         }
         // Pattern did NOT match.
         // Handle empty method (Empty implies "GET" in Go http.Request)
         if req.Method == "" {
            req.Method = "GET"
         }
         if parsedProxy != nil {
            log.Println("proxy", req.Method, req.URL)
            return parsedProxy, nil
         }
         // No proxy configured, but not excluded.
         log.Println(req.Method, req.URL)
         return nil, nil
      },
   }
   return nil
}

// detectDashType determines the file extension and container type from a DASH Representation's metadata.
func detectDashType(rep *dash.Representation) (*typeInfo, error) {
   switch rep.GetMimeType() {
   case "video/mp4":
      return &typeInfo{Extension: ".mp4", IsFMP4: true}, nil
   case "audio/mp4":
      return &typeInfo{Extension: ".m4a", IsFMP4: true}, nil
   case "text/vtt":
      return &typeInfo{Extension: ".vtt", IsFMP4: false}, nil
   default:
      return nil, fmt.Errorf("unsupported mime type for stream %s: %s", rep.Id, rep.GetMimeType())
   }
}

// detectHlsType finds the correct stream in an HLS playlist by its ID and determines its type.
func detectHlsType(playlist *hls.MasterPlaylist, streamId int) (*typeInfo, *url.URL, error) {
   // The string-to-int conversion is GONE.
   for _, variant := range playlist.StreamInfs {
      if variant.Id == streamId {
         info := &typeInfo{Extension: ".mp4", IsFMP4: true}
         return info, variant.Uri, nil
      }
   }
   for _, rendition := range playlist.Medias {
      if rendition.Id == streamId {
         var info *typeInfo
         switch rendition.Type {
         case "AUDIO":
            info = &typeInfo{Extension: ".m4a", IsFMP4: true}
         case "SUBTITLES":
            info = &typeInfo{Extension: ".vtt", IsFMP4: false}
         default:
            return nil, nil, fmt.Errorf("unsupported HLS media type: %s", rendition.Type)
         }
         return info, rendition.Uri, nil
      }
   }
   return nil, nil, fmt.Errorf("stream with ID not found: %d", streamId)
}

// typeInfo holds the determined properties of a media stream.
type typeInfo struct {
   Extension string
   IsFMP4    bool
}

// getSegment performs an HTTP GET request for a segment and returns its body.
func getSegment(targetUrl *url.URL, header http.Header) ([]byte, error) {
   req := http.Request{URL: targetUrl}
   if header != nil {
      req.Header = header
   } else {
      req.Header = http.Header{}
   }
   resp, err := http.DefaultClient.Do(&req)
   if err != nil {
      return nil, err
   }
   defer resp.Body.Close()
   if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
      return nil, errors.New(resp.Status)
   }
   return io.ReadAll(resp.Body)
}

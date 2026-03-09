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

func SetProxy(proxyUrl, excludePatterns string) error {
   var parsedProxy *url.URL
   if proxyUrl != "" {
      var err error
      parsedProxy, err = url.Parse(proxyUrl)
      if err != nil {
         return err
      }
   }
   // Split patterns by comma
   patterns := strings.Split(excludePatterns, ",")
   // Assign directly to the global DefaultTransport.
   // We ignore any existing values in the previous DefaultTransport.
   http.DefaultTransport = &http.Transport{
      DisableKeepAlives: true, // github.com/golang/go/issues/25793
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
func (c *Cache) Write(value any) error {
   bytes, err := xml.Marshal(value)
   if err != nil {
      return err
   }
   log.Println("Write", c.file)
   return os.WriteFile(c.file, bytes, os.ModePerm)
}

type Cache struct {
   file string
   // Memoization state
   read bool
   err  error
}

// Read hits the disk exactly once.
// allowMissing is optional.
// - If omitted or false: Strict (Returns error if missing).
// - If true: Lenient (Returns nil if missing).
func (c *Cache) Read(value any, allowMissing ...bool) error {
   // 1. One-time disk access
   if !c.read {
      var data []byte
      // Save file error only
      data, c.err = os.ReadFile(c.file)
      c.read = true
      // If read succeeded, parse immediately
      if c.err == nil {
         // XML errors are returned immediately and NOT stored in the struct
         if err := xml.Unmarshal(data, value); err != nil {
            return err
         }
      }
   }
   // 2. Handle File Errors (Cached)
   if c.err != nil {
      // Logic: If allowMissing is True, suppress the file error.
      if len(allowMissing) > 0 && allowMissing[0] {
         return nil
      }
      // Default strict behavior
      return c.err
   }
   return nil
}

// Update wrapper.
// NOTE: 'logic' must come before 'allowMissing' because variadic args must be last.
func (c *Cache) Update(value any, logic func() error, allowMissing ...bool) error {
   // Pass the optional bool down to Read
   if err := c.Read(value, allowMissing...); err != nil {
      return err
   }
   if err := logic(); err != nil {
      return err
   }
   return c.Write(value)
}

func ResolveCache(name string) (string, error) {
   root, err := os.UserCacheDir()
   if err != nil {
      return "", err
   }
   return filepath.Join(root, name), nil
}

// It relies on the struct's state for configuration.
func (c *Cache) Setup(name string) error {
   var err error
   c.file, err = ResolveCache(name)
   if err != nil {
      return err
   }
   // Create the directory immediately
   return os.MkdirAll(filepath.Dir(c.file), os.ModePerm)
}

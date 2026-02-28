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
   "path/filepath"
)

type Cache struct {
   path string
}

// Init takes a relative path, resolves it to the user cache dir, and creates the folder structure.
func (c *Cache) Init(path string) error {
   var err error
   c.path, err = ResolveCache(path)
   if err != nil {
      return err
   }
   return os.MkdirAll(filepath.Dir(c.path), os.ModePerm)
}

// ResolveCache joins the user cache directory with the provided path.
func ResolveCache(path string) (string, error) {
   baseDir, err := os.UserCacheDir()
   if err != nil {
      return "", err
   }
   return filepath.Join(baseDir, path), nil
}

// Set writes the value to the file at c.path
func (c *Cache) Set(value any) error {
   data, err := xml.Marshal(value)
   if err != nil {
      return err
   }
   log.Println("Saved:", c.path)
   return os.WriteFile(c.path, data, os.ModePerm)
}

// Get reads from the file at c.path into value
func (c *Cache) Get(value any) error {
   data, err := os.ReadFile(c.path)
   if err != nil {
      return err
   }
   return xml.Unmarshal(data, value)
}

// If Get returns an error, Update returns that error immediately
func (c *Cache) Update(value any, edit func() error) error {
   if err := c.Get(value); err != nil {
      return err
   }
   if err := edit(); err != nil {
      return err
   }
   return c.Set(value)
}

func SetProxy(resolve func(*http.Request) (string, bool)) {
   log.SetFlags(log.Ltime)
   http.DefaultTransport = &http.Transport{
      Protocols: &http.Protocols{},
      Proxy: func(req *http.Request) (*url.URL, error) {
         proxy, shouldLog := resolve(req)
         if shouldLog {
            if req.Method == "" {
               req.Method = http.MethodGet
            }
            if proxy != "" {
               log.Println("proxy", req.Method, req.URL)
            } else {
               // Log normally for direct connections
               log.Println(req.Method, req.URL)
            }
         }
         if proxy != "" {
            return url.Parse(proxy)
         }
         return nil, nil
      },
   }
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

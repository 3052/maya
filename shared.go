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
   dir string
}

func (c *Cache) Init(appName string) error {
   baseDir, err := os.UserCacheDir()
   if err != nil {
      return err
   }
   c.dir = filepath.Join(baseDir, appName)
   return nil
}

// Join returns the full path by joining the cache directory with the provided
// key
func (c *Cache) Join(key string) string {
   return filepath.Join(c.dir, key)
}

func (c *Cache) Set(key string, value any) error {
   data, err := xml.Marshal(value)
   if err != nil {
      return err
   }
   path := c.Join(key)
   // create the directory path based on the file location
   // filepath.Dir(path) ensures that if key is "nested/folder/file.xml",
   // the full folder structure is created.
   if err := os.MkdirAll(filepath.Dir(path), os.ModePerm); err != nil {
      return err
   }
   log.Println("Saved:", path)
   return os.WriteFile(path, data, os.ModePerm)
}

func (c *Cache) Get(key string, dest any) error {
   data, err := os.ReadFile(c.Join(key))
   if err != nil {
      return err
   }
   return xml.Unmarshal(data, dest)
}

func createFile(name string) (*os.File, error) {
   err := os.MkdirAll(filepath.Dir(name), os.ModePerm)
   if err != nil {
      return nil, err
   }
   log.Println("Creating file:", name)
   return os.Create(name)
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

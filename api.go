package maya

import (
   "encoding/xml"
   "flag"
   "fmt"
   "log"
   "net/http"
   "net/url"
   "os"
   "path"
   "path/filepath"
   "strings"
)

func Parse() map[string]bool {
   flag.Parse()
   set := map[string]bool{}
   flag.Visit(func(f *flag.Flag) {
      set[f.Name] = true
   })
   return set
}

// BoolVar sets up a bool flag and returns its reference.
func BoolVar(value *bool, name, usage string) *flag.Flag {
   flag.BoolVar(value, name, *value, usage)
   return flag.Lookup(name)
}

// IntVar sets up an int flag and returns its reference.
func IntVar(value *int, name, usage string) *flag.Flag {
   flag.IntVar(value, name, *value, usage)
   return flag.Lookup(name)
}

// StringVar sets up a string flag and returns its reference.
func StringVar(value *string, name, usage string) *flag.Flag {
   flag.StringVar(value, name, *value, usage)
   return flag.Lookup(name)
}

// Usage prints the structured usage and checks for missing flags.
func Usage(groups [][]*flag.Flag) error {
   seen := map[string]bool{}
   // 1. Print usage and mark flags as seen
   for i, group := range groups {
      if i >= 1 {
         fmt.Println()
      }
      for _, f := range group {
         fmt.Printf("-%v %v\n", f.Name, f.Usage)
         if f.DefValue != "" {
            fmt.Printf("\tdefault %v\n", f.DefValue)
         }
         seen[f.Name] = true
      }
   }
   // 2. Check for missing flags
   var missing string
   flag.VisitAll(func(f *flag.Flag) {
      if !seen[f.Name] {
         missing = f.Name
      }
   })
   if missing != "" {
      return fmt.Errorf("defined flag missing: -%s", missing)
   }
   return nil
}

// ListDash parses a DASH manifest and lists the available streams.
func ListDash(body []byte, baseURL *url.URL) error {
   manifest, err := parseDash(body, baseURL)
   if err != nil {
      return err
   }
   return listStreamsDash(manifest)
}

// ListHls parses an HLS playlist and lists the available streams.
func ListHls(body []byte, baseURL *url.URL) error {
   playlist, err := parseHls(body, baseURL)
   if err != nil {
      return err
   }
   return listStreamsHls(playlist)
}

// Sender encapsulates the process of sending a byte payload (like a signed
// license request) to a DRM server and returning the response payload.
type Sender func([]byte) ([]byte, error)

// Job holds configuration for downloads.
// Widevine and PlayReady specify folder paths containing their respective keys.
type Job struct {
   Threads   int
   Widevine  string
   PlayReady string
}

// DownloadDash parses and downloads a DASH stream (Clear or Encrypted).
func (j *Job) DownloadDash(body []byte, baseURL *url.URL, streamId string, send Sender) error {
   manifest, err := parseDash(body, baseURL)
   if err != nil {
      return err
   }

   fetcher, err := j.getFetcher(send)
   if err != nil {
      return err
   }

   return downloadDash(manifest, j.Threads, streamId, fetcher)
}

// DownloadHls parses and downloads an HLS stream (Clear or Encrypted).
func (j *Job) DownloadHls(body []byte, baseURL *url.URL, streamId int, send Sender) error {
   playlist, err := parseHls(body, baseURL)
   if err != nil {
      return err
   }

   fetcher, err := j.getFetcher(send)
   if err != nil {
      return err
   }

   return downloadHls(playlist, j.Threads, streamId, fetcher)
}

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

type Cache struct {
   File string
}

func (c *Cache) Setup(file string) error {
   var err error
   c.File, err = os.UserCacheDir()
   if err != nil {
      return err
   }
   c.File = filepath.Join(c.File, file)
   return os.MkdirAll(filepath.Dir(c.File), os.ModePerm)
}

func (c *Cache) Read(value any) error {
   data, err := os.ReadFile(c.File)
   if err != nil {
      return err
   }
   return xml.Unmarshal(data, value)
}

func (c *Cache) Write(value any) error {
   data, err := xml.MarshalIndent(value, "", " ")
   if err != nil {
      return err
   }
   log.Println("Write", c.File)
   return os.WriteFile(c.File, data, os.ModePerm)
}

// api.go
package maya

import (
   "encoding/xml"
   "fmt"
   "log"
   "net/http"
   "net/url"
   "os"
   "path"
   "path/filepath"
   "slices"
   "strconv"
   "strings"
)

type Flag struct {
   Name   string
   IsBool bool
   IsSet  bool
   Set    func(string) error
   Usage  string
}

var flags []*Flag

func FuncFlag(name, usage string, fn func(string) error) *Flag {
   f := &Flag{
      Name:  name,
      Set:   fn,
      Usage: fmt.Sprintf(" value\n\t%s", usage),
   }
   flags = append(flags, f)
   return f
}

func StringFlag(pointer *string, name, usage string) *Flag {
   usage = fmt.Sprintf(" string\n\t%s", usage)
   if *pointer != "" {
      usage += fmt.Sprintf("\n\tdefault %s", *pointer)
   }

   f := &Flag{
      Name: name,
      Set: func(val string) error {
         *pointer = val
         return nil
      },
      Usage: usage,
   }
   flags = append(flags, f)
   return f
}

func BoolFlag(name, usage string) *Flag {
   f := &Flag{
      Name:   name,
      IsBool: true,
      Usage:  fmt.Sprintf("\n\t%s", usage),
   }
   flags = append(flags, f)
   return f
}

func IntFlag(pointer *int, name, usage string) *Flag {
   usage = fmt.Sprintf(" int\n\t%s", usage)
   if *pointer != 0 {
      usage += fmt.Sprintf("\n\tdefault %d", *pointer)
   }

   f := &Flag{
      Name: name,
      Set: func(val string) (err error) {
         *pointer, err = strconv.Atoi(val)
         return
      },
      Usage: usage,
   }
   flags = append(flags, f)
   return f
}

func ParseFlags() error {
   for i := 1; i < len(os.Args); i++ {
      arg := os.Args[i]

      if len(arg) < 2 || arg[0] != '-' {
         return fmt.Errorf("unexpected argument: %s", arg)
      }

      name := arg[1:]

      idx := slices.IndexFunc(flags, func(f *Flag) bool {
         return f.Name == name
      })

      if idx == -1 {
         return fmt.Errorf("provided but not defined: -%s", name)
      }
      f := flags[idx]

      if !f.IsBool {
         i++
         if i >= len(os.Args) {
            return fmt.Errorf("flag needs an argument: -%s", name)
         }

         if err := f.Set(os.Args[i]); err != nil {
            return fmt.Errorf("invalid value for flag -%s: %v", name, err)
         }
      }

      f.IsSet = true
   }
   return nil
}

func PrintFlags(groups [][]*Flag) error {
   printed := make(map[*Flag]bool)

   for i, group := range groups {
      if i > 0 {
         fmt.Fprintln(os.Stderr)
      }
      for _, f := range group {
         fmt.Fprintf(os.Stderr, "-%s%s\n", f.Name, f.Usage)
         printed[f] = true
      }
   }

   for _, f := range flags {
      if !printed[f] {
         return fmt.Errorf("flag -%s is missing from PrintFlags groups", f.Name)
      }
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

// Fetcher encapsulates the process of fetching a byte payload (like a signed
// license request) from a DRM server and returning the response payload.
type Fetcher func([]byte) ([]byte, error)

// Job holds configuration for downloads.
// Widevine and PlayReady specify folder paths containing their respective keys.
type Job struct {
   Threads   int
   Widevine  string
   PlayReady string
}

// DownloadDash parses and downloads a DASH stream (Clear or Encrypted).
func (j *Job) DownloadDash(body []byte, baseURL *url.URL, streamId string, fetch Fetcher) error {
   manifest, err := parseDash(body, baseURL)
   if err != nil {
      return err
   }

   fetcher, err := j.getFetcher(fetch)
   if err != nil {
      return err
   }

   return downloadDash(manifest, j.Threads, streamId, fetcher)
}

// DownloadHls parses and downloads an HLS stream (Clear or Encrypted).
func (j *Job) DownloadHls(body []byte, baseURL *url.URL, streamId int, fetch Fetcher) error {
   playlist, err := parseHls(body, baseURL)
   if err != nil {
      return err
   }

   fetcher, err := j.getFetcher(fetch)
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

func (c *Cache) Read(value any) func(func() error) error {
   // 1. Attempt the read and unmarshal, capturing any error
   data, err := os.ReadFile(c.File)
   if err == nil {
      err = xml.Unmarshal(data, value)
   }
   // 2. Return the callback wrapper
   return func(action func() error) error {
      if err != nil {
         return err // Blocks the action and returns the read error
      }
      return action()
   }
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

func (c *Cache) Write(value any) error {
   data, err := xml.MarshalIndent(value, "", " ")
   if err != nil {
      return err
   }
   log.Println("Write", c.File)
   return os.WriteFile(c.File, data, os.ModePerm)
}

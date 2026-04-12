package main

import (
   "encoding/json"
   "errors"
   "flag"
   "fmt"
   "io"
   "net/http"
   "net/http/cookiejar"
   "net/url"
   "os"
   "path/filepath"
   "time"
)

// Representation represents a stubbed out DASH Representation
type Representation struct {
   ID         string
   Resolution string
   Bandwidth  int
   Codecs     string
}

// CachePayload holds both the manifest bytes and the session cookies
type CachePayload struct {
   Manifest []byte         `json:"manifest"`
   Cookies  []*http.Cookie `json:"cookies"`
}

func main() {
   urlFlag := flag.String("url", "", "The URL of the DASH MPD manifest (required for DASH actions)")
   listFlag := flag.Bool("list", false, "Print available representations")
   downloadFlag := flag.String("download", "", "ID of the representation to download")
   // Future unrelated flags can go here (e.g., otherFlag := flag.Bool("other", false, "..."))

   flag.Usage = func() {
      fmt.Fprintf(os.Stderr, "Usage of %s:\n", os.Args[0])
      fmt.Fprintf(os.Stderr, "  %s -url <DASH_URL> [-list] [-download <REP_ID>]\n\n", os.Args[0])
      flag.PrintDefaults()
   }
   flag.Parse()

   // Track if any known action was requested
   actionExecuted := false

   // Variables to hold DASH session state (lazily initialized)
   var client *http.Client
   var manifestBytes []byte

   // ensureDashSession initializes the HTTP client, cookie jar, and fetches the manifest
   // EXACTLY ONCE, only when a DASH-related action is actually triggered.
   ensureDashSession := func() {
      if client != nil && manifestBytes != nil {
         return // Already initialized
      }
      if *urlFlag == "" {
         fmt.Println("Error: -url flag is required for -list or -download.")
         flag.Usage()
         os.Exit(1)
      }

      var err error
      client, manifestBytes, err = initDashSession(*urlFlag)
      if err != nil {
         fmt.Printf("Failed to initialize DASH session: %v\n", err)
         os.Exit(1)
      }
   }

   // Action: List Representations
   if *listFlag {
      actionExecuted = true
      ensureDashSession()

      representations, err := parseRepresentations(manifestBytes)
      if err != nil {
         fmt.Printf("Error parsing manifest: %v\n", err)
         os.Exit(1)
      }

      fmt.Println("\nAvailable Representations:")
      for _, rep := range representations {
         fmt.Printf("  - ID: %-10s | Resolution: %-10s | Bandwidth: %-8d bps | Codecs: %s\n",
            rep.ID, rep.Resolution, rep.Bandwidth, rep.Codecs)
      }
   }

   // Action: Download Chosen Representation
   if *downloadFlag != "" {
      actionExecuted = true
      ensureDashSession()

      err := downloadRepresentation(client, *urlFlag, manifestBytes, *downloadFlag)
      if err != nil {
         fmt.Printf("Error downloading representation: %v\n", err)
         os.Exit(1)
      }
   }

   // Future Actions could be evaluated here without triggering DASH setup...
   // if *otherFlag {
   //     actionExecuted = true
   //     doOtherThing()
   // }

   // If no flags were provided at all, print usage
   if !actionExecuted {
      flag.Usage()
      os.Exit(1)
   }
}

// ---------------------------------------------------------
// DASH SESSION SETUP & CACHING
// ---------------------------------------------------------

// initDashSession sets up the cookie jar, HTTP client, and retrieves the manifest.
func initDashSession(manifestURL string) (*http.Client, []byte, error) {
   jar, err := cookiejar.New(nil)
   if err != nil {
      return nil, nil, fmt.Errorf("error creating cookie jar: %w", err)
   }

   client := &http.Client{
      Jar:     jar,
      Timeout: 15 * time.Second,
   }

   manifestBytes, err := getManifestAndCookies(client, manifestURL)
   if err != nil {
      return nil, nil, err
   }

   return client, manifestBytes, nil
}

// getManifestAndCookies uses a single JSON file to cache both the MPD and Cookies.
func getManifestAndCookies(client *http.Client, manifestURL string) ([]byte, error) {
   parsedURL, err := url.Parse(manifestURL)
   if err != nil {
      return nil, fmt.Errorf("invalid URL: %w", err)
   }

   cacheFilePath := filepath.Join(os.TempDir(), "dash_cache.json")

   // 1. Try to load the combined payload from Cache
   fileData, err := os.ReadFile(cacheFilePath)
   if err == nil {
      fmt.Printf("Using locally cached session data: %s\n", cacheFilePath)
      var payload CachePayload
      
      if unmarshalErr := json.Unmarshal(fileData, &payload); unmarshalErr != nil {
         fmt.Printf("Warning: Failed to parse cache file (%v). Re-downloading...\n", unmarshalErr)
      } else {
         if len(payload.Cookies) > 0 {
            client.Jar.SetCookies(parsedURL, payload.Cookies)
            fmt.Printf("Successfully loaded %d cached cookies into the session.\n", len(payload.Cookies))
         }
         return payload.Manifest, nil
      }
   } else if !errors.Is(err, os.ErrNotExist) {
      fmt.Printf("Warning: Could not read cache file (%v). Re-downloading...\n", err)
   }

   // 2. Cache miss -> Download from network
   fmt.Printf("Downloading manifest from network: %s\n", manifestURL)
   
   req, err := http.NewRequest("GET", manifestURL, nil)
   if err != nil {
      return nil, fmt.Errorf("failed to create request: %w", err)
   }
   req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; DashDownloader/1.0)")

   resp, err := client.Do(req)
   if err != nil {
      return nil, fmt.Errorf("failed to download manifest: %w", err)
   }
   
   defer func() {
      if closeErr := resp.Body.Close(); closeErr != nil {
         fmt.Fprintf(os.Stderr, "Warning: failed to close response body: %v\n", closeErr)
      }
   }()

   if resp.StatusCode < 200 || resp.StatusCode >= 300 {
      return nil, fmt.Errorf("server returned non-success status: %s", resp.Status)
   }

   manifestBytes, err := io.ReadAll(resp.Body)
   if err != nil {
      return nil, fmt.Errorf("failed to read manifest body: %w", err)
   }

   fmt.Printf("Successfully downloaded manifest (%d bytes).\n", len(manifestBytes))

   // 3. Save both Manifest and Cookies to a single cache file
   cookies := client.Jar.Cookies(parsedURL)
   payload := CachePayload{
      Manifest: manifestBytes,
      Cookies:  cookies,
   }

   cacheData, err := json.Marshal(payload)
   if err != nil {
      fmt.Printf("Warning: Failed to marshal cache payload: %v\n", err)
   } else {
      if writeErr := os.WriteFile(cacheFilePath, cacheData, 0644); writeErr != nil {
         fmt.Printf("Warning: Failed to write cache file to disk: %v\n", writeErr)
      } else {
         fmt.Printf("Saved manifest and %d cookies to unified cache file.\n", len(cookies))
      }
   }

   return manifestBytes, nil
}

// ---------------------------------------------------------
// STUBBED DASH LOGIC BELOW
// ---------------------------------------------------------

func parseRepresentations(manifestBytes []byte) ([]Representation, error) {
   dummyData := []Representation{
      {ID: "video_1080p", Resolution: "1920x1080", Bandwidth: 5000000, Codecs: "avc1.640028"},
      {ID: "video_720p", Resolution: "1280x720", Bandwidth: 2500000, Codecs: "avc1.4d401f"},
      {ID: "video_480p", Resolution: "854x480", Bandwidth: 1000000, Codecs: "avc1.42c01e"},
      {ID: "audio_en", Resolution: "N/A", Bandwidth: 128000, Codecs: "mp4a.40.2"},
   }
   return dummyData, nil
}

func downloadRepresentation(client *http.Client, manifestURL string, manifestBytes []byte, repID string) error {
   fmt.Printf("\nInitializing download for representation: %s\n", repID)

   parsedURL, err := url.Parse(manifestURL)
   if err != nil {
      return fmt.Errorf("failed to parse manifest URL in downloader: %w", err)
   }

   activeCookies := client.Jar.Cookies(parsedURL)
   if len(activeCookies) > 0 {
      fmt.Printf("[Stub info] Client has %d cookies ready to send with segment requests:\n", len(activeCookies))
      for _, c := range activeCookies {
         fmt.Printf("  - %s=%s\n", c.Name, c.Value)
      }
   } else {
      fmt.Println("[Stub info] No cookies are currently attached to this session.")
   }

   fmt.Print("\nDownloading segments: [========================================] 100%\n")
   fmt.Printf("Successfully downloaded representation '%s' to output.mp4\n", repID)
   
   return nil
}

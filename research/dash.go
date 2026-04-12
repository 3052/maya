package main

import (
   "crypto/md5"
   "encoding/hex"
   "encoding/json"
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

func main() {
   urlFlag := flag.String("url", "", "The URL of the DASH MPD manifest (required)")
   listFlag := flag.Bool("list", false, "Print available representations")
   downloadFlag := flag.String("download", "", "ID of the representation to download")

   flag.Usage = func() {
      fmt.Fprintf(os.Stderr, "Usage of %s:\n", os.Args[0])
      fmt.Fprintf(os.Stderr, "  %s -url <DASH_URL> [-list] [-download <REP_ID>]\n\n", os.Args[0])
      flag.PrintDefaults()
   }
   flag.Parse()

   if *urlFlag == "" {
      fmt.Println("Error: -url flag is required.")
      flag.Usage()
      os.Exit(1)
   }

   if !*listFlag && *downloadFlag == "" {
      fmt.Println("Error: You must specify an action: either -list, -download <id>, or both.")
      flag.Usage()
      os.Exit(1)
   }

   // 1. Setup the HTTP Client with a Cookie Jar
   // This ensures cookies are automatically saved and sent on future requests.
   jar, err := cookiejar.New(nil)
   if err != nil {
      fmt.Printf("Error creating cookie jar: %v\n", err)
      os.Exit(1)
   }

   client := &http.Client{
      Jar:     jar,
      Timeout: 15 * time.Second,
   }

   // 2. Fetch the manifest & populate cookies (handles caching for both)
   manifestBytes, err := getManifestAndCookies(client, *urlFlag)
   if err != nil {
      fmt.Printf("Error fetching manifest: %v\n", err)
      os.Exit(1)
   }

   // Action: List Representations
   if *listFlag {
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
      // Pass the configured `client` so the stub can make segment requests WITH cookies.
      err := downloadRepresentation(client, *urlFlag, manifestBytes, *downloadFlag)
      if err != nil {
         fmt.Printf("Error downloading representation: %v\n", err)
         os.Exit(1)
      }
   }
}

// ---------------------------------------------------------
// HTTP, COOKIE & CACHING LOGIC
// ---------------------------------------------------------

// getManifestAndCookies checks local disk cache first, falling back to HTTP download if missing.
// It ensures the client.Jar is populated with cookies either from the network or the cache.
func getManifestAndCookies(client *http.Client, manifestURL string) ([]byte, error) {
   parsedURL, err := url.Parse(manifestURL)
   if err != nil {
      return nil, fmt.Errorf("invalid URL: %w", err)
   }

   hash := md5.Sum([]byte(manifestURL))
   hashStr := hex.EncodeToString(hash[:])
   
   cacheManifestPath := filepath.Join(os.TempDir(), fmt.Sprintf("dash_%s.mpd", hashStr))
   cacheCookiePath := filepath.Join(os.TempDir(), fmt.Sprintf("dash_%s_cookies.json", hashStr))

   // 1. Try to load Manifest and Cookies from Cache
   if info, err := os.Stat(cacheManifestPath); err == nil && !info.IsDir() {
      fmt.Printf("Using locally cached manifest: %s\n", cacheManifestPath)
      
      manifestBytes, err := os.ReadFile(cacheManifestPath)
      if err != nil {
         return nil, err
      }

      // Load cached cookies into the client's jar
      if cookieData, err := os.ReadFile(cacheCookiePath); err == nil {
         var cachedCookies []*http.Cookie
         if err := json.Unmarshal(cookieData, &cachedCookies); err == nil {
            client.Jar.SetCookies(parsedURL, cachedCookies)
            fmt.Println("Successfully loaded cached cookies into the session.")
         }
      }

      return manifestBytes, nil
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
   defer resp.Body.Close()

   if resp.StatusCode < 200 || resp.StatusCode >= 300 {
      return nil, fmt.Errorf("server returned non-success status: %s", resp.Status)
   }

   manifestBytes, err := io.ReadAll(resp.Body)
   if err != nil {
      return nil, fmt.Errorf("failed to read manifest body: %w", err)
   }

   fmt.Printf("Successfully downloaded manifest (%d bytes).\n", len(manifestBytes))

   // 3. Save Manifest and Cookies to cache for the next run
   os.WriteFile(cacheManifestPath, manifestBytes, 0644)
   
   cookies := client.Jar.Cookies(parsedURL)
   if len(cookies) > 0 {
      if cookieData, err := json.Marshal(cookies); err == nil {
         os.WriteFile(cacheCookiePath, cookieData, 0644)
         fmt.Printf("Saved %d cookies to cache.\n", len(cookies))
      }
   }

   return manifestBytes, nil
}

// ---------------------------------------------------------
// STUBBED DASH LOGIC BELOW
// ---------------------------------------------------------

func parseRepresentations(manifestBytes []byte) ([]Representation, error) {
   // TODO: Parse the XML in `manifestBytes` and extract actual representations here.

   dummyData := []Representation{
      {ID: "video_1080p", Resolution: "1920x1080", Bandwidth: 5000000, Codecs: "avc1.640028"},
      {ID: "video_720p", Resolution: "1280x720", Bandwidth: 2500000, Codecs: "avc1.4d401f"},
      {ID: "video_480p", Resolution: "854x480", Bandwidth: 1000000, Codecs: "avc1.42c01e"},
      {ID: "audio_en", Resolution: "N/A", Bandwidth: 128000, Codecs: "mp4a.40.2"},
   }

   return dummyData, nil
}

// downloadRepresentation now accepts the configured `*http.Client` so it can use the cookies.
func downloadRepresentation(client *http.Client, manifestURL string, manifestBytes []byte, repID string) error {
   fmt.Printf("\nInitializing download for representation: %s\n", repID)

   // Demonstration that the client has the required cookies ready for segment downloads
   parsedURL, _ := url.Parse(manifestURL)
   activeCookies := client.Jar.Cookies(parsedURL)
   if len(activeCookies) > 0 {
      fmt.Printf("[Stub info] Client has %d cookies ready to send with segment requests:\n", len(activeCookies))
      for _, c := range activeCookies {
         fmt.Printf("  - %s=%s\n", c.Name, c.Value)
      }
   } else {
      fmt.Println("[Stub info] No cookies are currently attached to this session.")
   }

   // TODO: Parse the XML in `manifestBytes`, find the Representation matching `repID`, 
   // resolve the SegmentTemplate (using manifestURL as the base URL for relative paths).
   //
   // IMPORTANT: When you build the segment HTTP requests, you MUST use `client.Do(req)`
   // so that the cookies are automatically attached to the segment requests!
   
   fmt.Print("\nDownloading segments: [========================================] 100%\n")
   fmt.Printf("Successfully downloaded representation '%s' to output.mp4\n", repID)
   
   return nil
}

package maya

import (
   "os"
   "strings"
   "testing"
)

func TestParseFlags(t *testing.T) {
   originalArgs := os.Args
   defer func() { os.Args = originalArgs }()

   type Config struct {
      VerboseOutput Flag
      HelloWorld    StringFlag `group:"User"`
      ServerPort    IntFlag    `group:"Server"`
      ApiEndpoint   UrlFlag    `group:"Server"`
   }

   // 1. Valid flags using partial matches
   os.Args = []string{"app", "Verbose", "World", "John", "ServerPort", "8080", "ApiEndpoint", "https://api.example.com/v1"}
   var cfg Config

   if err := ParseFlags(&cfg); err != nil {
      t.Fatalf("expected successful parse, got err: %v", err)
   }

   if !cfg.VerboseOutput.Set || !cfg.HelloWorld.Set || cfg.HelloWorld.Value != "John" {
      t.Fatalf("parse state incorrect: %+v", cfg)
   }

   if cfg.ServerPort.Value != 8080 {
      t.Fatalf("ParseInt failed: got %d", cfg.ServerPort.Value)
   }

   if cfg.ApiEndpoint.Value.Scheme != "https" || cfg.ApiEndpoint.Value.Host != "api.example.com" {
      t.Fatalf("ParseUrl failed: got %+v", cfg.ApiEndpoint.Value)
   }
}

func TestParseFlags_IntError(t *testing.T) {
   originalArgs := os.Args
   defer func() { os.Args = originalArgs }()

   type Config struct {
      ServerPort IntFlag
   }

   os.Args = []string{"app", "ServerPort", "not_a_number"}
   var cfg Config

   err := ParseFlags(&cfg)
   if err == nil {
      t.Fatalf("expected ParseFlags to fail for invalid int, got nil")
   }

   if !strings.Contains(err.Error(), `invalid value "not_a_number" for flag ServerPort`) {
      t.Fatalf("unexpected ParseInt error format: %v", err)
   }
}

func TestParseFlags_UrlError(t *testing.T) {
   originalArgs := os.Args
   defer func() { os.Args = originalArgs }()

   type Config struct {
      ApiUrl UrlFlag
   }

   // Control characters force url.Parse to fail
   os.Args = []string{"app", "ApiUrl", "http://example.com/api\x00"}
   var cfg Config

   err := ParseFlags(&cfg)
   if err == nil {
      t.Fatalf("expected ParseFlags to fail for invalid url, got nil")
   }

   if !strings.Contains(err.Error(), `invalid value "http://example.com/api\x00" for flag ApiUrl`) {
      t.Fatalf("unexpected ParseUrl error format: %v", err)
   }
}

func TestFormatFlags_NoGroups(t *testing.T) {
   type Config struct {
      VerboseOutput Flag
      HelloWorld    StringFlag
      ServerPort    IntFlag
   }

   var cfg Config

   os.Stdout.WriteString("\n--- FORMAT OUTPUT (NO GROUPS) ---\n")
   if err := FormatFlags(&cfg); err != nil {
      t.Fatalf("expected no error from FormatFlags, got: %v", err)
   }
   os.Stdout.WriteString("---------------------------------\n\n")
}

func TestFormatFlags_AllGroups(t *testing.T) {
   type Config struct {
      HelloWorld  StringFlag `group:"User Options"`
      HelloPlanet StringFlag `group:"User Options"`
      ServerHost  StringFlag `group:"Server Settings"`
      ServerPort  IntFlag    `group:"Server Settings"`
   }

   var cfg Config

   os.Stdout.WriteString("\n--- FORMAT OUTPUT (ALL GROUPS) ---\n")
   if err := FormatFlags(&cfg); err != nil {
      t.Fatalf("expected no error from FormatFlags, got: %v", err)
   }
   os.Stdout.WriteString("----------------------------------\n\n")
}

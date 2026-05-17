package myflag

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
      HelloWorld    Flag `value:"true" group:"User"`
      ServerPort    Flag `value:"true" group:"Server"`
      ApiEndpoint   Flag `value:"true" group:"Server"`
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

   // Test ParseInt()
   port, err := cfg.ServerPort.ParseInt()
   if err != nil || port != 8080 {
      t.Fatalf("ParseInt failed: got %d, err %v", port, err)
   }

   // Test ParseUrl()
   parsedUrl, err := cfg.ApiEndpoint.ParseUrl()
   if err != nil || parsedUrl.Scheme != "https" || parsedUrl.Host != "api.example.com" {
      t.Fatalf("ParseUrl failed: got %+v, err %v", parsedUrl, err)
   }
}

func TestParseMethods_Errors(t *testing.T) {
   originalArgs := os.Args
   defer func() { os.Args = originalArgs }()

   type Config struct {
      ServerPort Flag `value:"true"`
      ApiUrl     Flag `value:"true"`
   }

   // Deliberately passing invalid values
   os.Args = []string{"app", "ServerPort", "not_a_number", "ApiUrl", "http://%"}
   var cfg Config

   if err := ParseFlags(&cfg); err != nil {
      t.Fatalf("expected successful parse, got err: %v", err)
   }

   // Verify Int formatting (dash removed)
   _, err := cfg.ServerPort.ParseInt()
   if err == nil {
      t.Fatalf("expected ParseInt to fail, got nil")
   }
   if !strings.Contains(err.Error(), `invalid value "not_a_number" for flag ServerPort`) {
      t.Fatalf("unexpected ParseInt error format: %v", err)
   }

   // Verify URL formatting (dash removed)
   _, err = cfg.ApiUrl.ParseUrl()
   if err == nil {
      t.Fatalf("expected ParseUrl to fail, got nil")
   }
   if !strings.Contains(err.Error(), `invalid value "http://%" for flag ApiUrl`) {
      t.Fatalf("unexpected ParseUrl error format: %v", err)
   }
}

func TestFormatFlags_NoGroups(t *testing.T) {
   type Config struct {
      VerboseOutput Flag
      HelloWorld    Flag `value:"true"`
      ServerPort    Flag `value:"true"`
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
      HelloWorld  Flag `value:"true" group:"User Options"`
      HelloPlanet Flag `value:"true" group:"User Options"`
      ServerHost  Flag `value:"true" group:"Server Settings"`
      ServerPort  Flag `value:"true" group:"Server Settings"`
   }

   var cfg Config

   os.Stdout.WriteString("\n--- FORMAT OUTPUT (ALL GROUPS) ---\n")
   if err := FormatFlags(&cfg); err != nil {
      t.Fatalf("expected no error from FormatFlags, got: %v", err)
   }
   os.Stdout.WriteString("----------------------------------\n\n")
}

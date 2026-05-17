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
      User          struct {
         HelloWorld StringFlag
      }
      Server struct {
         ServerPort  IntFlag
         ApiEndpoint UrlFlag
      }
   }

   // 1. Valid flags using partial matches across top-level and nested structs
   os.Args = []string{"app", "Verbose", "World", "John", "ServerPort", "8080", "ApiEndpoint", "https://api.example.com/v1"}
   var cfg Config

   if err := ParseFlags(&cfg); err != nil {
      t.Fatalf("expected successful parse, got err: %v", err)
   }

   if !cfg.VerboseOutput.Set || !cfg.User.HelloWorld.Set || cfg.User.HelloWorld.Value != "John" {
      t.Fatalf("parse state incorrect: %+v", cfg)
   }

   if cfg.Server.ServerPort.Value != 8080 {
      t.Fatalf("ParseInt failed: got %d", cfg.Server.ServerPort.Value)
   }

   if cfg.Server.ApiEndpoint.Value.Scheme != "https" || cfg.Server.ApiEndpoint.Value.Host != "api.example.com" {
      t.Fatalf("ParseUrl failed: got %+v", cfg.Server.ApiEndpoint.Value)
   }
}

func TestParseFlags_IntError(t *testing.T) {
   originalArgs := os.Args
   defer func() { os.Args = originalArgs }()

   type Config struct {
      Server struct {
         ServerPort IntFlag
      }
   }

   os.Args = []string{"app", "ServerPort", "not_a_number"}
   var cfg Config

   err := ParseFlags(&cfg)
   if err == nil {
      t.Fatalf("expected ParseFlags to fail for invalid int, got nil")
   }

   if !strings.Contains(err.Error(), `invalid value "not_a_number" for flag ServerPort`) {
      t.Fatalf("unexpected error format: %v", err)
   }
}

func TestFormatFlags_MixedGroups(t *testing.T) {
   type Config struct {
      VerboseOutput Flag
      HelloWorld    StringFlag
      Server        struct {
         ServerPort  IntFlag
         ApiEndpoint UrlFlag
      }
      UserOptions struct {
         Age IntFlag
      }
   }

   var cfg Config

   os.Stdout.WriteString("\n--- FORMAT OUTPUT ---\n")
   if err := FormatFlags(&cfg); err != nil {
      t.Fatalf("expected no error from FormatFlags, got: %v", err)
   }
   os.Stdout.WriteString("---------------------\n\n")
}

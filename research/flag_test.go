package myflag

import (
   "os"
   "strings"
   "testing"
)

func TestParseFlags(t *testing.T) {
   type Config struct {
      VerboseOutput Flag[bool]
      _             FlagSpace
      HelloWorld    Flag[string]
      _             FlagSpace
      ServerPort    Flag[int]
   }

   // 1. Valid flags using partial matches
   args := []string{"Verbose", "World", "John", "ServerPort", "8080"}
   var cfg Config

   if err := ParseFlags(&cfg, args); err != nil {
      t.Fatalf("expected successful parse, got err: %v", err)
   }

   if !cfg.VerboseOutput.Set || !cfg.HelloWorld.Set || cfg.HelloWorld.Value != "John" {
      t.Fatalf("parse state incorrect: %+v", cfg)
   }

   if cfg.ServerPort.Value != 8080 {
      t.Fatalf("ParseInt failed: got %d", cfg.ServerPort.Value)
   }
}

func TestParseFlags_IntError(t *testing.T) {
   type Config struct {
      ServerPort Flag[int]
   }

   args := []string{"ServerPort", "not_a_number"}
   var cfg Config

   err := ParseFlags(&cfg, args)
   if err == nil {
      t.Fatalf("expected ParseFlags to fail for invalid int, got nil")
   }

   if !strings.Contains(err.Error(), `invalid value "not_a_number" for flag ServerPort`) {
      t.Fatalf("unexpected ParseInt error format: %v", err)
   }
}

func TestFormatFlags_WithExamples(t *testing.T) {
   type Config struct {
      HelloWorld  Flag[string]
      HelloPlanet Flag[bool]
      _           FlagSpace
      ServerHost  Flag[string]
      ServerPort  Flag[int]
   }

   var cfg Config

   os.Stdout.WriteString("\n--- FORMAT OUTPUT (WITH EXAMPLES) ---\n")
   err := FormatFlags(&cfg,
      "program Planet",
      "program Host example.com",
   )
   if err != nil {
      t.Fatalf("expected no error from FormatFlags, got: %v", err)
   }
   os.Stdout.WriteString("-------------------------------------\n\n")
}

func TestFormatFlags_NoExamples(t *testing.T) {
   type Config struct {
      VerboseOutput Flag[bool]
      HelloWorld    Flag[string]
   }

   var cfg Config

   os.Stdout.WriteString("\n--- FORMAT OUTPUT (NO EXAMPLES) ---\n")
   if err := FormatFlags(&cfg); err != nil {
      t.Fatalf("expected no error from FormatFlags, got: %v", err)
   }
   os.Stdout.WriteString("-----------------------------------\n\n")
}

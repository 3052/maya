package maya

import (
   "os"
   "strings"
   "testing"
)

func TestParseFlags(t *testing.T) {
   type Config struct {
      VerboseOutput Flag[bool]
      HelloWorld    Flag[string]
      ServerPort    Flag[int]
   }

   // 1. Valid flags using prefix matches
   // "Verbose" -> matches "VerboseOutput"
   // "HelloW"  -> matches "HelloWorld"
   args := []string{"Verbose", "HelloW", "John", "ServerPort", "8080"}
   var cfg Config

   if err := ParseFlags(args, &cfg); err != nil {
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

   err := ParseFlags(args, &cfg)
   if err == nil {
      t.Fatalf("expected ParseFlags to fail for invalid int, got nil")
   }

   if !strings.Contains(err.Error(), `invalid flag ServerPort: strconv.Atoi: parsing "not_a_number": invalid syntax`) {
      t.Fatalf("unexpected ParseInt error format: %v", err)
   }
}

func TestFormatFlags_WithExamples(t *testing.T) {
   type Config struct {
      HelloWorld  Flag[string]
      HelloPlanet Flag[bool]
      ServerHost  Flag[string]
      ServerPort  Flag[int]
   }

   var cfg Config

   os.Stdout.WriteString("\n--- FORMAT OUTPUT (WITH EXAMPLES) ---\n")
   err := FormatFlags(os.Stdout, &cfg,
      "program HelloP",
      "program ServerH example.com",
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
   if err := FormatFlags(os.Stdout, &cfg); err != nil {
      t.Fatalf("expected no error from FormatFlags, got: %v", err)
   }
   os.Stdout.WriteString("-----------------------------------\n\n")
}

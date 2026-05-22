package myflag

import (
   "bytes"
   "os"
   "strings"
   "testing"
)

type Config struct {
   Verbose Flag[bool]
   Name    Flag[string]
   Role    Flag[string]
   Count   Flag[int]
}

func TestParseFlags(t *testing.T) {
   cfg := Config{
      Verbose: Flag[bool]{Usage: "enable verbose output"},
      Name:    Flag[string]{Usage: "user name"},
      Role:    Flag[string]{Value: "guest", Usage: "user role"},
      Count:   Flag[int]{Value: 1, Usage: "number of items"},
   }

   args := []string{"Verbose", "Name", "admin", "Count", "42"}

   err := ParseFlags(args, &cfg)
   if err != nil {
      t.Fatalf("expected no error, got: %v", err)
   }

   if !cfg.Verbose.Set || cfg.Verbose.Value != true {
      t.Errorf("expected Verbose to be Set and true")
   }
   if cfg.Name.Value != "admin" {
      t.Errorf("expected Name to be 'admin', got '%s'", cfg.Name.Value)
   }
   if cfg.Role.Value != "guest" {
      t.Errorf("expected Role to remain 'guest', got '%s'", cfg.Role.Value)
   }
   if !cfg.Count.Set || cfg.Count.Value != 42 {
      t.Errorf("expected Count to be Set and 42, got %d", cfg.Count.Value)
   }
}

func TestPrintFlags(t *testing.T) {
   cfg := Config{
      Verbose: Flag[bool]{Usage: "enable verbose output"},
      Name:    Flag[string]{Usage: "user name"},
      Count:   Flag[int]{Usage: "number of items"},
   }

   var buf bytes.Buffer
   err := PrintFlags(&buf, &cfg)
   if err != nil {
      t.Fatalf("expected no error, got: %v", err)
   }

   out := buf.String()

   if !strings.Contains(out, "  Verbose\n    \tenable verbose output") {
      t.Errorf("output missing Verbose formatting, got:\n%s", out)
   }
   if !strings.Contains(out, "  Name\n    \tuser name") {
      t.Errorf("output missing Name formatting, got:\n%s", out)
   }
   if !strings.Contains(out, "  Count\n    \tnumber of items") {
      t.Errorf("output missing Count formatting, got:\n%s", out)
   }

   // Ensure it still prints directly to stderr without error
   err = PrintFlags(os.Stderr, &cfg)
   if err != nil {
      t.Fatalf("expected no error printing to stderr, got: %v", err)
   }
}

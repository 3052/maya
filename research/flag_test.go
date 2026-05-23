package myflag

import (
   "bytes"
   "io"
   "os"
   "strings"
   "testing"
)

func TestParseFlags(t *testing.T) {
   type Config struct {
      Verbose Flag[bool]
      DryRun  Flag[bool]
      Name    Flag[string]
      Role    Flag[string]
      Count   Flag[int]
      Limit   Flag[int]
      Address Flag[string]
      Age     int // Non-flag field to ensure we skip it properly
   }

   cfg := Config{
      // Initializing with non-zero defaults to ensure empty syntax properly overwrites them to zero
      Verbose: Flag[bool]{Value: true},
      DryRun:  Flag[bool]{Value: false},
      Name:    Flag[string]{Value: "default_name"},
      Role:    Flag[string]{Value: "default_role"},
      Count:   Flag[int]{Value: 99},
      Limit:   Flag[int]{Value: 99},
      Address: Flag[string]{Value: "default_address"},
   }

   args := []string{
      "Verb",       // bool with no equals -> false, HasEqual: false
      "Dry=true",   // bool with value -> true, HasEqual: true
      "Na",         // string with no equals -> "", HasEqual: false
      "Role=admin", // string with value -> "admin", HasEqual: true
      "Cou=",       // int with empty equals -> 0, HasEqual: true
      "Limit=42",   // int with value -> 42, HasEqual: true
   }

   err := ParseFlags(args, &cfg)
   if err != nil {
      t.Fatalf("expected no error, got: %v", err)
   }

   if !cfg.Verbose.Set || cfg.Verbose.Value != false || cfg.Verbose.HasEqual != false {
      t.Errorf("expected Verbose to be Set, false, HasEqual false")
   }
   if !cfg.DryRun.Set || cfg.DryRun.Value != true || cfg.DryRun.HasEqual != true {
      t.Errorf("expected DryRun to be Set, true, HasEqual true")
   }
   if !cfg.Name.Set || cfg.Name.Value != "" || cfg.Name.HasEqual != false {
      t.Errorf("expected Name to be Set, empty string, HasEqual false")
   }
   if !cfg.Role.Set || cfg.Role.Value != "admin" || cfg.Role.HasEqual != true {
      t.Errorf("expected Role to be Set, 'admin', HasEqual true")
   }
   if !cfg.Count.Set || cfg.Count.Value != 0 || cfg.Count.HasEqual != true {
      t.Errorf("expected Count to be Set, 0, HasEqual true")
   }
   if !cfg.Limit.Set || cfg.Limit.Value != 42 || cfg.Limit.HasEqual != true {
      t.Errorf("expected Limit to be Set, 42, HasEqual true")
   }
   if cfg.Address.Set {
      t.Errorf("expected Address to NOT be set")
   }
}

func TestPrintFlags(t *testing.T) {
   type Config struct {
      Verbose Flag[bool]
      Name    Flag[string]
      Count   Flag[int]
      Limit   Flag[int]
      Season  Flag[string]
      Age     int
   }

   cfg := Config{
      Verbose: Flag[bool]{Usage: "enable verbose output", Value: true},
      Name:    Flag[string]{Usage: "user name", Value: "guest"},
      Count:   Flag[int]{Usage: "number of items", Value: 10},
      Limit:   Flag[int]{Usage: "limit of items"}, // Zero value implicitly set to 0
      Season:  Flag[string]{Usage: "season", Requires: "Address", Value: "Fall"},
   }

   var buf bytes.Buffer
   w := io.MultiWriter(os.Stderr, &buf)

   err := PrintFlags(w, &cfg)
   if err != nil {
      t.Fatalf("expected no error, got: %v", err)
   }

   out := buf.String()

   if !strings.Contains(out, "Verbose\n\tenable verbose output (default true)") {
      t.Errorf("output missing Verbose formatting, got:\n%s", out)
   }
   if !strings.Contains(out, "Name\n\tuser name (default \"guest\")") {
      t.Errorf("output missing Name formatting, got:\n%s", out)
   }
   if !strings.Contains(out, "Count\n\tnumber of items (default 10)") {
      t.Errorf("output missing Count formatting, got:\n%s", out)
   }
   if !strings.Contains(out, "Limit\n\tlimit of items\n") {
      t.Errorf("output missing Limit formatting (should hide default 0), got:\n%s", out)
   }
   if !strings.Contains(out, "Season\n\tseason (default \"Fall\")") {
      t.Errorf("output missing Season formatting, got:\n%s", out)
   }
   if strings.Contains(out, "Age") {
      t.Errorf("output incorrectly included Age field")
   }
}

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
   Address Flag[string]
   Season  Flag[string]
   Age     int // Non-flag field to ensure we skip it properly
}

func TestParseFlags(t *testing.T) {
   cfg := Config{
      Verbose: Flag[bool]{Usage: "enable verbose output"},
      Name:    Flag[string]{Usage: "user name"},
      Role:    Flag[string]{Value: "guest", Usage: "user role"},
      Count:   Flag[int]{Value: 1, Usage: "number of items"},
      Address: Flag[string]{Usage: "user address"},
      Season:  Flag[string]{Usage: "season", Requires: []string{"Address"}},
   }

   args := []string{"Verb", "Na", "admin", "Cou", "42", "Add", "123 Main St", "Sea", "Summer"}

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
   if !cfg.Address.Set || cfg.Address.Value != "123 Main St" {
      t.Errorf("expected Address to be Set and '123 Main St', got '%s'", cfg.Address.Value)
   }
   if !cfg.Season.Set || cfg.Season.Value != "Summer" {
      t.Errorf("expected Season to be Set and 'Summer', got '%s'", cfg.Season.Value)
   }
   if len(cfg.Season.Requires) != 1 || cfg.Season.Requires[0] != "Address" {
      t.Errorf("expected Season to require 'Address', got %v", cfg.Season.Requires)
   }
}

func TestParseFlags_Ambiguous(t *testing.T) {
   type AmbigConfig struct {
      App   Flag[bool]
      Apple Flag[bool]
   }

   cfgAmbig := AmbigConfig{}
   err := ParseFlags([]string{"Ap"}, &cfgAmbig)
   if err == nil {
      t.Fatalf("expected error for ambiguous flag, got nil")
   }
   expectedErr := `flag "Ap" is ambiguous`
   if err.Error() != expectedErr {
      t.Errorf("expected error %q, got %q", expectedErr, err.Error())
   }
}

func TestPrintFlags(t *testing.T) {
   cfg := Config{
      Verbose: Flag[bool]{Usage: "enable verbose output"},
      Name:    Flag[string]{Usage: "user name"},
      Count:   Flag[int]{Usage: "number of items"},
      Season:  Flag[string]{Usage: "season", Requires: []string{"Address"}},
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
   if !strings.Contains(out, "  Season\n    \tseason") {
      t.Errorf("output missing Season formatting, got:\n%s", out)
   }
   if strings.Contains(out, "Age") {
      t.Errorf("output incorrectly included Age field")
   }

   err = PrintFlags(os.Stderr, &cfg)
   if err != nil {
      t.Fatalf("expected no error printing to stderr, got: %v", err)
   }
}

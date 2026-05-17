package myflag

import (
   "os"
   "testing"
)

func TestParse(t *testing.T) {
   // Save original os.Args and restore it after the test finishes
   originalArgs := os.Args
   defer func() { os.Args = originalArgs }()

   // We use ",hasvalue" to explicitly declare which flags require a following argument
   type Config struct {
      Verbose Flag   `flag:"verbose"`       // No value required
      Name    *Flag  `flag:"name,hasvalue"` // Requires a value (e.g. text)
      Port    Flag   `flag:"port,hasvalue"` // Requires a value (e.g. number)
      Ignored string // No tag, should be ignored
   }

   // 1. Valid flags (mixing flags with values and flags without values)
   os.Args = []string{"app", "verbose", "name", "John", "port", "8080"}
   var cfg1 Config
   if err := Parse(&cfg1); err != nil || !cfg1.Verbose.IsSet || !cfg1.Name.IsSet || cfg1.Name.Value != "John" || cfg1.Port.Value != "8080" {
      t.Fatalf("expected successful parse, got err: %v, state: %+v", err, cfg1)
   }

   // 2. Missing value (port is missing its number)
   os.Args = []string{"app", "port"}
   var cfg2 Config
   if err := Parse(&cfg2); err == nil {
      t.Fatal("expected error for missing value, got nil")
   }

   // 3. Unknown flag
   os.Args = []string{"app", "fakeflag"}
   var cfg3 Config
   if err := Parse(&cfg3); err == nil {
      t.Fatal("expected error for unknown flag, got nil")
   }

   // 4. Not a pointer to struct (passing by value)
   var cfg4 Config
   if err := Parse(cfg4); err == nil {
      t.Fatal("expected error for passing by value, got nil")
   }

   // 5. Unsupported field type mapped to a flag tag
   var unsupportedConfig struct {
      Age int `flag:"age"`
   }
   if err := Parse(&unsupportedConfig); err == nil {
      t.Fatal("expected error for unsupported field type, got nil")
   }
}

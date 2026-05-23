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

   type AmbigConfig struct {
      App   Flag[bool]
      Apple Flag[bool]
   }

   cfgAmbig := AmbigConfig{}
   err = ParseFlags([]string{"Ap=true"}, &cfgAmbig)
   if err == nil {
      t.Fatalf("expected error for ambiguous flag, got nil")
   }
   expectedErr := `flag "Ap" is ambiguous`
   if err.Error() != expectedErr {
      t.Errorf("expected error %q, got %q", expectedErr, err.Error())
   }
}

func TestPrintFlags(t *testing.T) {
   type Config struct {
      WidevineFolder Flag[string]
      SetProxy       Flag[string]
      Address        Flag[string]
      Season         Flag[int]
      MubiId         Flag[int]
      DashId         Flag[string]
      Verbose        Flag[bool] // Added bool flag
      Age            int
   }

   cfg := Config{
      Season: Flag[int]{Requires: "Address"},
   }

   var buf bytes.Buffer
   w := io.MultiWriter(os.Stderr, &buf)

   err := PrintFlags(w, "mubi", &cfg)
   if err != nil {
      t.Fatalf("expected no error, got: %v", err)
   }

   out := buf.String()

   expectedParts := []string{
      "Index:\n",
      "\tWidevineFolder string\n",
      "\tSetProxy string\n",
      "\tAddress string\n",
      "\tSeason int\n",
      "\tMubiId int\n",
      "\tDashId string\n",
      "\tVerbose\n", // Bool type is omitted
      "\nExamples:\n",
      "\tmubi W=xyz\n",
      "\tmubi SetProxy=xyz\n",
      "\tmubi A=xyz\n",
      "\tmubi A=xyz Season=789\n", // Shows dependency correctly
      "\tmubi M=789\n",
      "\tmubi D=xyz\n",
      "\tmubi V\n", // Bool has no equals syntax
   }

   for _, part := range expectedParts {
      if !strings.Contains(out, part) {
         t.Errorf("output missing expected part: %q\nGot:\n%s", part, out)
      }
   }

   if strings.Contains(out, "Age") {
      t.Errorf("output incorrectly included Age field")
   }
}

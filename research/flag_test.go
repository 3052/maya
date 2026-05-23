// maya_test.go
package maya

import (
   "testing"
)

func TestFlagSetParse_Success(t *testing.T) {
   hostFlag := new(Flag).SetName("host").SetValue("localhost").SetUsage("server host address")
   portFlag := new(Flag).SetName("port").SetValue("8080").SetUsage("server port number").SetRequires(hostFlag)
   verboseFlag := new(Flag).SetName("verbose").SetValue("false").SetUsage("enable verbose logging")
   timeoutFlag := new(Flag).SetName("timeout").SetValue("30s").SetUsage("connection timeout")

   fs := FlagSet{hostFlag, portFlag, verboseFlag, timeoutFlag}

   // Testing prefix matching: "ho" matches "host", "po" matches "port", "ver" matches "verbose"
   args := []string{"ho=127.0.0.1", "po=", "ver"}
   err := fs.Parse(args)

   if err != nil {
      t.Fatalf("unexpected error during parse: %v", err)
   }

   if f := fs.Lookup("host"); f.Value != "127.0.0.1" || !f.Set || !f.HasEqual || f.Usage != "server host address" {
      t.Errorf("host flag state invalid, got Value: '%s', Set: %v, HasEqual: %v, Usage: '%s'", f.Value, f.Set, f.HasEqual, f.Usage)
   }

   if f := fs.Lookup("port"); f.Value != "" || !f.Set || !f.HasEqual {
      t.Errorf("expected port to be empty string, Set: true, HasEqual: true, got '%s', Set: %v, HasEqual: %v", f.Value, f.Set, f.HasEqual)
   }
   if f := fs.Lookup("port"); f.Requires == nil || f.Requires.Name != "host" {
      t.Errorf("expected port flag to require host flag, got %v", f.Requires)
   }

   if f := fs.Lookup("verbose"); f.Value != "" || !f.Set || f.HasEqual {
      t.Errorf("expected verbose to be empty string, Set: true, HasEqual: false, got '%s', Set: %v, HasEqual: %v", f.Value, f.Set, f.HasEqual)
   }

   if f := fs.Lookup("timeout"); f.Value != "30s" || f.Set || f.HasEqual {
      t.Errorf("expected timeout to be '30s', Set: false, HasEqual: false, got '%s', Set: %v, HasEqual: %v", f.Value, f.Set, f.HasEqual)
   }
}

func TestFlagSetParse_Ambiguous(t *testing.T) {
   fs := FlagSet{
      new(Flag).SetName("host").SetValue("localhost"),
      new(Flag).SetName("house").SetValue("suburb"),
   }

   // "ho" is a valid prefix for both "host" and "house"
   err := fs.Parse([]string{"ho=127.0.0.1"})

   if err == nil {
      t.Fatal("expected error due to ambiguous flag, got nil")
   }

   expected := "ambiguous argument: ho"
   if err.Error() != expected {
      t.Errorf("expected error '%s', got '%s'", expected, err.Error())
   }
}

// maya_test.go
package maya

import (
   "fmt"
   "os"
   "strings"
   "testing"
)

func TestFlagSetParse_Success(t *testing.T) {
   hostFlag := new(Flag).SetName("host").SetValue("localhost").SetUsage("server host address").SetNeedsValue(true)
   portFlag := new(Flag).SetName("port").SetValue("8080").SetUsage("server port number").SetNeeds(hostFlag).SetNeedsValue(true)
   verboseFlag := new(Flag).SetName("verbose").SetValue("false").SetUsage("enable verbose logging").SetNeedsValue(false)
   timeoutFlag := new(Flag).SetName("timeout").SetValue("30s").SetUsage("connection timeout").SetNeedsValue(true)

   fs := FlagSet{hostFlag, portFlag, verboseFlag, timeoutFlag}

   // "ho" needs value, "po" needs value, "ver" strictly takes no value
   args := []string{"ho=127.0.0.1", "po=", "ver"}
   err := fs.Parse(args)

   if err != nil {
      t.Fatalf("unexpected error during parse: %v", err)
   }

   if hostFlag.Value != "127.0.0.1" || !hostFlag.Set || hostFlag.Usage != "server host address" {
      t.Errorf("host flag state invalid, got Value: '%s', Set: %v, Usage: '%s'", hostFlag.Value, hostFlag.Set, hostFlag.Usage)
   }

   if portFlag.Value != "" || !portFlag.Set {
      t.Errorf("expected port to be empty string, Set: true, got '%s', Set: %v", portFlag.Value, portFlag.Set)
   }
   if portFlag.Needs == nil || portFlag.Needs.Name != "host" {
      t.Errorf("expected port flag to need host flag, got %v", portFlag.Needs)
   }

   if verboseFlag.Value != "" || !verboseFlag.Set {
      t.Errorf("expected verbose to be empty string, Set: true, got '%s', Set: %v", verboseFlag.Value, verboseFlag.Set)
   }

   if timeoutFlag.Value != "30s" || timeoutFlag.Set {
      t.Errorf("expected timeout to be '30s', Set: false, got '%s', Set: %v", timeoutFlag.Value, timeoutFlag.Set)
   }
}

func TestFlagSetParse_EmptyNameError(t *testing.T) {
   fs := FlagSet{
      new(Flag).SetName("host").SetNeedsValue(true),
      new(Flag).SetName(""), // Empty name
   }

   err := fs.Parse([]string{"host=127.0.0.1"})
   if err == nil {
      t.Fatal("expected error due to empty flag name, got nil")
   }

   if err.Error() != "flag name cannot be empty" {
      t.Errorf("expected error 'flag name cannot be empty', got '%s'", err.Error())
   }
}

func TestFlagSetUsage_Success(t *testing.T) {
   hostFlag := new(Flag).SetName("host").SetValue("localhost").SetUsage("server host address").SetNeedsValue(true)

   fs := FlagSet{
      hostFlag,
      new(Flag).SetName("port").SetValue("8080").SetUsage("server port number").SetNeeds(hostFlag).SetNeedsValue(true),
      new(Flag).SetName("proxy").SetUsage("proxy address").SetNeedsValue(true), // Added to make 'p' non-unique
      new(Flag).SetName("verbose").SetUsage("enable verbose logging").SetNeedsValue(false),
      new(Flag).SetName("retries").SetValue("3").SetNeedsValue(true),
      new(Flag).SetName("silent").SetNeedsValue(false),
   }

   data := new(strings.Builder)
   err := fs.Usage(data, "mubi")
   if err != nil {
      t.Fatalf("unexpected error generating usage: %v", err)
   }
   usage := data.String()

   // Output to stderr to verify visual layout
   fmt.Fprint(os.Stderr, usage)

   // Validate Index Section
   if !strings.Contains(usage, "Index:\n") {
      t.Errorf("expected usage to contain Index header")
   }
   if !strings.Contains(usage, "\thost value\n\t\tserver host address (default: localhost)\n") {
      t.Errorf("expected usage to contain formatted host details in index")
   }
   if !strings.Contains(usage, "\tport value\n\t\tserver port number (default: 8080)\n") {
      t.Errorf("expected usage to contain formatted port details in index")
   }
   if !strings.Contains(usage, "\tsilent\n") || strings.Contains(usage, "silent\n\t\t") {
      t.Errorf("expected silent usage to have no extra indentation in index")
   }

   // Validate Examples Section
   if !strings.Contains(usage, "Examples:\n") {
      t.Errorf("expected usage to contain Examples header")
   }

   // 'h' is unique (host)
   if !strings.Contains(usage, "\tmubi h=value\n") {
      t.Errorf("expected host example to use unique prefix 'h'")
   }

   // 'p' is NOT unique (port, proxy) and requires host
   if !strings.Contains(usage, "\tmubi h=value port=value\n") {
      t.Errorf("expected port example to use full name and include required host")
   }
   if !strings.Contains(usage, "\tmubi proxy=value\n") {
      t.Errorf("expected proxy example to use full name")
   }

   // 'v' is unique (verbose) - takes no value
   if !strings.Contains(usage, "\tmubi v\n") {
      t.Errorf("expected verbose example to use unique prefix 'v' with no value")
   }
}

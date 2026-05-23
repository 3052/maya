// maya_test.go
package maya

import (
   "fmt"
   "os"
   "strconv"
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

func TestFlagSetUsage(t *testing.T) {
   fs := FlagSet{
      new(Flag).SetName("host").SetValue("localhost").SetUsage("server host address").SetNeedsValue(true),
      new(Flag).SetName("port").SetValue("8080").SetUsage("server port number").SetNeedsValue(true),
      new(Flag).SetName("verbose").SetUsage("enable verbose logging").SetNeedsValue(false),
      new(Flag).SetName("retries").SetValue("3").SetNeedsValue(true),
      new(Flag).SetName("silent").SetNeedsValue(false),
   }

   usage := fs.Usage()

   // Output to stderr to verify visual layout
   fmt.Fprint(os.Stderr, usage)

   if !strings.Contains(usage, "host value\n\tserver host address (default: localhost)\n") {
      t.Errorf("expected usage to contain formatted host details, got:\n%s", usage)
   }
   if !strings.Contains(usage, "port value\n\tserver port number (default: 8080)\n") {
      t.Errorf("expected usage to contain formatted port details, got:\n%s", usage)
   }
   if !strings.Contains(usage, "verbose\n\tenable verbose logging\n") {
      t.Errorf("expected verbose usage to exist correctly, got:\n%s", usage)
   }
   if !strings.Contains(usage, "retries value\n\t(default: 3)\n") {
      t.Errorf("expected retries usage to exist with only the default string, got:\n%s", usage)
   }
   if !strings.Contains(usage, "silent\n") || strings.Contains(usage, "silent\n\t") {
      t.Errorf("expected silent usage to have no extra indentation, got:\n%s", usage)
   }
}

func TestParseFlag(t *testing.T) {
   portFlag := new(Flag).SetName("port").SetValue("8080")
   verboseFlag := new(Flag).SetName("verbose").SetValue("true")

   // Test converting to int
   port, err := ParseFlag(portFlag, strconv.Atoi)
   if err != nil {
      t.Fatalf("unexpected error converting port: %v", err)
   }
   if port != 8080 {
      t.Errorf("expected port to be 8080, got %d", port)
   }

   // Test converting to bool
   verbose, err := ParseFlag(verboseFlag, strconv.ParseBool)
   if err != nil {
      t.Fatalf("unexpected error converting verbose: %v", err)
   }
   if !verbose {
      t.Errorf("expected verbose to be true, got %v", verbose)
   }
}

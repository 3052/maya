package miniflag

import (
   "bytes"
   "io"
   "os"
   "strings"
   "testing"
)

func TestParseSuccess(t *testing.T) {
   var host StringValue = "localhost"
   var port IntValue = 8080
   var verbose BoolValue = false
   var dryRun BoolValue = true

   set := FlagSet{
      &Flag{Name: "host", Usage: "Server host", Value: &host},
      &Flag{Name: "port", Usage: "Server port", Value: &port},
      &Flag{Name: "verbose", Usage: "Enable verbose logging", Value: &verbose},
      &Flag{Name: "dry-run", Usage: "Simulate only", Value: &dryRun},
   }

   // Using prefix matching! "ho" matches "host", "po" matches "port", "verb" matches "verbose", etc.
   args := []string{"ho=127.0.0.1", "po=9090", "verb", "dry=false"}

   err := set.Parse(args)
   if err != nil {
      t.Fatalf("unexpected error: %v", err)
   }

   if host != "127.0.0.1" {
      t.Errorf("expected host 127.0.0.1, got %s", host)
   }
   if port != 9090 {
      t.Errorf("expected port 9090, got %d", port)
   }
   if verbose != true {
      t.Errorf("expected verbose to be true, got %t", verbose)
   }
   if dryRun != false {
      t.Errorf("expected dry-run to be false, got %t", dryRun)
   }

   if !set.IsSet(&host) {
      t.Errorf("expected host to be set")
   }
   if !set.IsSet(&verbose) {
      t.Errorf("expected verbose to be set")
   }

   var dummy StringValue
   if set.IsSet(&dummy) {
      t.Errorf("dummy value should not be set")
   }
}

func TestParseAmbiguous(t *testing.T) {
   var verbosity IntValue
   var verbose BoolValue

   set := FlagSet{
      &Flag{Name: "verbosity", Value: &verbosity},
      &Flag{Name: "verbose", Value: &verbose},
   }

   // "verb" matches BOTH "verbose" and "verbosity"
   err := set.Parse([]string{"verb"})
   if err == nil {
      t.Fatalf("expected error for ambiguous flag, got nil")
   }

   if !strings.Contains(err.Error(), "ambiguous flag: verb") {
      t.Errorf("expected ambiguous flag error, got: %v", err)
   }
}

func TestUsage(t *testing.T) {
   var host StringValue = "127.0.0.1"
   var verbose BoolValue = false // zero value, default omitted
   var hidden IntValue = 42      // no usage, but has default
   var secret StringValue = ""   // no usage, no default

   set := FlagSet{
      &Flag{
         Name:  "host",
         Usage: "The server host",
         Value: &host,
      },
      &Flag{
         Name:  "verbose",
         Usage: "Enable verbose output",
         Value: &verbose,
      },
      &Flag{
         Name:  "hidden",
         Usage: "",
         Value: &hidden,
      },
      &Flag{
         Name:  "secret",
         Usage: "",
         Value: &secret,
      },
   }

   var buf bytes.Buffer
   w := io.MultiWriter(os.Stderr, &buf)

   err := set.Usage(w)
   if err != nil {
      t.Fatalf("unexpected error writing usage: %v", err)
   }

   expected := "host string\n\tThe server host (default 127.0.0.1)\n" +
      "verbose bool\n\tEnable verbose output\n" +
      "hidden int\n\t(default 42)\n" +
      "secret string\n"

   if buf.String() != expected {
      t.Errorf("Usage output mismatch.\nExpected:\n%q\nGot:\n%q", expected, buf.String())
   }
}

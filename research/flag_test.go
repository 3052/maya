package miniflag

import (
   "bytes"
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

   // Testing missing equal sign (verbose) and explicit equal sign (dry-run=false)
   args := []string{"host=127.0.0.1", "port=9090", "verbose", "dry-run=false"}

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

   // Verify IsSet works
   if !set.IsSet(&host) {
      t.Errorf("expected host to be set")
   }
   if !set.IsSet(&verbose) {
      t.Errorf("expected verbose to be set")
   }

   // Verify an unparsed value is not set
   var dummy StringValue
   if set.IsSet(&dummy) {
      t.Errorf("dummy value should not be set")
   }
}

func TestUsage(t *testing.T) {
   var host StringValue = "127.0.0.1"
   var verbose BoolValue = false

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
   }

   var buf bytes.Buffer
   err := set.Usage(&buf)
   if err != nil {
      t.Fatalf("unexpected error writing usage: %v", err)
   }

   expected := "host string (default: 127.0.0.1)\n\tThe server host\n" +
      "verbose bool (default: false)\n\tEnable verbose output\n"

   if buf.String() != expected {
      t.Errorf("Usage output mismatch.\nExpected:\n%q\nGot:\n%q", expected, buf.String())
   }
}

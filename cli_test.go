package maya

import (
   "bytes"
   "io"
   "os"
   "testing"
)

func TestParseSuccess(t *testing.T) {
   var host FlagString = "localhost"
   var port FlagInt = 8080
   var verbose FlagBool = false
   var dryRun FlagBool = true

   set := FlagSet{
      &Flag{Name: "host", Usage: "Server host", Value: &host},
      &Flag{Name: "port", Usage: "Server port", Value: &port},
      &Flag{Name: "verbose", Usage: "Enable verbose logging", Value: &verbose},
      &Flag{Name: "dry-run", Usage: "Simulate only", Value: &dryRun},
   }

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

   var dummy FlagString
   if set.IsSet(&dummy) {
      t.Errorf("dummy value should not be set")
   }
}

func TestUsage(t *testing.T) {
   var linkCode FlagBool = false // no usage, no default
   var mubiID FlagInt = 123      // no usage, default
   var address FlagString = ""   // usage, no default
   var season FlagInt = 9        // usage, default

   set := FlagSet{
      &Flag{
         Name:  "link-code",
         Usage: "",
         Value: &linkCode,
      },
      &Flag{
         Name:  "mubi-id",
         Usage: "",
         Value: &mubiID,
      },
      &Flag{
         Name:  "address",
         Usage: "The network address",
         Value: &address,
      },
      &Flag{
         Name:  "season",
         Usage: "The season number",
         Value: &season,
         Needs: "address",
      },
   }

   var buf bytes.Buffer
   w := io.MultiWriter(os.Stderr, &buf)

   err := set.Usage(w, "mubi")
   if err != nil {
      t.Fatalf("unexpected error writing usage: %v", err)
   }

   expected := "index:\n" +
      "     link-code bool\n" +
      "     mubi-id int\n" +
      "          default: 123\n" +
      "     address string\n" +
      "          usage: The network address\n" +
      "     season int\n" +
      "          usage: The season number\n" +
      "          default: 9\n" +
      "\n" +
      "examples:\n" +
      "     mubi l\n" +
      "     mubi m=789\n" +
      "     mubi a=xyz\n" +
      "     mubi a=xyz s=789\n"

   if buf.String() != expected {
      t.Errorf("Usage output mismatch.\nExpected:\n%q\nGot:\n%q", expected, buf.String())
   }
}

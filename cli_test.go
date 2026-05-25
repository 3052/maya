package maya

import (
   "bytes"
   "io"
   "os"
   "strings"
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

func TestParseAmbiguous(t *testing.T) {
   var verbosity FlagInt
   var verbose FlagBool

   set := FlagSet{
      &Flag{Name: "verbosity", Value: &verbosity},
      &Flag{Name: "verbose", Value: &verbose},
   }

   err := set.Parse([]string{"verb"})
   if err == nil {
      t.Fatalf("expected error for ambiguous flag, got nil")
   }

   if !strings.Contains(err.Error(), "ambiguous flag: verb") {
      t.Errorf("expected ambiguous flag error, got: %v", err)
   }
}

func TestUsage(t *testing.T) {
   var address FlagString = ""
   var season FlagInt = 0
   var session FlagBool = false

   set := FlagSet{
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
      &Flag{
         Name:  "session",
         Usage: "Enable the session",
         Value: &session,
      },
   }

   var buf bytes.Buffer
   w := io.MultiWriter(os.Stderr, &buf)

   err := set.Usage(w, "mubi")
   if err != nil {
      t.Fatalf("unexpected error writing usage: %v", err)
   }

   expected := "Index:\n" +
      "\taddress string\n" +
      "\t\tThe network address\n" +
      "\tseason int\n" +
      "\t\tThe season number\n" +
      "\tsession bool\n" +
      "\t\tEnable the session\n" +
      "\n" +
      "Examples:\n" +
      "\tmubi a=xyz\n" +
      "\tmubi a=xyz season=789\n" +
      "\tmubi session\n"

   if buf.String() != expected {
      t.Errorf("Usage output mismatch.\nExpected:\n%q\nGot:\n%q", expected, buf.String())
   }
}

func TestUsageNeedsError(t *testing.T) {
   var season FlagInt = 0

   set := FlagSet{
      &Flag{
         Name:  "season",
         Usage: "The season number",
         Value: &season,
         Needs: "missing",
      },
   }

   var buf bytes.Buffer
   err := set.Usage(&buf, "mubi")
   if err == nil {
      t.Fatalf("expected error for missing dependency, got nil")
   }
   if !strings.Contains(err.Error(), "needs undefined flag") {
      t.Errorf("expected missing flag error, got: %v", err)
   }
}

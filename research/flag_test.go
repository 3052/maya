package flag

import (
   "testing"
)

func TestParse_Valid(t *testing.T) {
   var fs FlagSet
   fs.String(&Flag[string]{Name: "host", Value: "localhost", Usage: "database host"})
   fs.Int(&Flag[int]{Name: "port", Value: 5432, Usage: "database port"})
   fs.Int(&Flag[int]{Name: "timeout", Value: 30, Usage: "timeout in seconds"})

   args := []string{
      "host=127.0.0.1",
      "port=8080",
   }

   err := fs.Parse(args)
   if err != nil {
      t.Fatalf("unexpected error: %v", err)
   }

   if val := fs.Strings[0].Value; val != "127.0.0.1" {
      t.Errorf("expected host '127.0.0.1', got '%s'", val)
   }
   if val := fs.Ints[0].Value; val != 8080 {
      t.Errorf("expected port 8080, got %d", val)
   }
   // timeout should remain default
   if val := fs.Ints[1].Value; val != 30 {
      t.Errorf("expected timeout 30, got %d", val)
   }
}

func TestParse_MissingValue(t *testing.T) {
   var fs FlagSet
   fs.String(&Flag[string]{Name: "host", Value: "localhost"})

   // "host" without "=" should result in value == ""
   err := fs.Parse([]string{"host"})
   if err != nil {
      t.Fatalf("unexpected error: %v", err)
   }

   if val := fs.Strings[0].Value; val != "" {
      t.Errorf("expected empty string, got '%s'", val)
   }
}

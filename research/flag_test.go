package flag

import (
   "fmt"
   "testing"
)

func TestParse(t *testing.T) {
   nameVal := &StringValue{}
   ageVal := &IntValue{}
   emptyVal := &StringValue{}

   fs := FlagSet{
      &Flag{Name: "name", Usage: "user name", Value: nameVal},
      &Flag{Name: "age", Usage: "user age", Value: ageVal},
      &Flag{Name: "empty", Usage: "empty string flag", Value: emptyVal},
   }

   // Parse a standard set of arguments
   args := []string{"name=Alice", "age=30", "empty="}
   err := fs.Parse(args)
   if err != nil {
      t.Fatalf("unexpected error parsing valid arguments: %v", err)
   }

   // Check string value
   if nameVal.Value != "Alice" || !nameVal.IsSet {
      t.Errorf("expected name to be 'Alice' and IsSet to be true, got %q (IsSet: %v)", nameVal.Value, nameVal.IsSet)
   }

   // Check int value
   if ageVal.Value != 30 || !ageVal.IsSet {
      t.Errorf("expected age to be 30 and IsSet to be true, got %d (IsSet: %v)", ageVal.Value, ageVal.IsSet)
   }

   // Check empty value parsing
   if emptyVal.Value != "" || !emptyVal.IsSet {
      t.Errorf("expected empty string and IsSet to be true, got %q (IsSet: %v)", emptyVal.Value, emptyVal.IsSet)
   }

   // Ensure error handling works for invalid types
   badArgs := []string{"age=thirty"}
   if err := fs.Parse(badArgs); err == nil {
      t.Error("expected an error when passing non-integer to int flag, got nil")
   }

   // Ensure error handling works for undefined flags
   unknownArgs := []string{"unknown=foo"}
   if err := fs.Parse(unknownArgs); err == nil {
      t.Error("expected an error when passing undefined flag, got nil")
   }
}

func TestPrintDefaultsOutput(t *testing.T) {
   fs := FlagSet{
      &Flag{
         Name:  "host",
         Usage: "server host",
         // Pre-setting a default value to demonstrate PrintDefaults
         Value: &StringValue{Value: "localhost"},
      },
      &Flag{
         Name:  "port",
         Usage: "server port",
         // Pre-setting a default value to demonstrate PrintDefaults
         Value: &IntValue{Value: 8080},
      },
   }

   fmt.Println("\n--- Inspect PrintDefaults Output ---")
   fs.PrintDefaults()
   fmt.Println("------------------------------------")
}

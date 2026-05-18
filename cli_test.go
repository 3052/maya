package maya

import (
   "bytes"
   "fmt"
   "strings"
   "testing"
)

// client is our test struct reflecting the exact scenario discussed.
type client struct {
   WidevineFolder Flag[string]
   SetProxy       Flag[string]
   UseProxy       Flag[bool] `depends:"MubiId"`
   LinkCode       Flag[bool]
   Session        Flag[bool]
   Address        Flag[string]
   Season         Flag[int] `depends:"Address"`
   MubiId         Flag[int]
   DashId         Flag[string]
}

func TestFormatFlags(t *testing.T) {
   var c client
   var buf bytes.Buffer

   // Call FormatFlags with our struct and provide "myapp" as the command name
   err := FormatFlags(&buf, "myapp", &c)
   if err != nil {
      t.Fatalf("FormatFlags returned an unexpected error: %v", err)
   }

   output := buf.String()

   // Print the output to the console so you can see the auto-generated menu!
   fmt.Println("--- Auto-Generated Help Menu ---")
   fmt.Print(output)
   fmt.Println("--------------------------------")

   // 1. Test that standalone string and int examples generated correctly
   if !strings.Contains(output, "\tmyapp A xyz\n") {
      t.Errorf("Expected output to contain standalone Address example (shortened to 'A'). Got:\n%s", output)
   }
   if !strings.Contains(output, "\tmyapp M 123\n") {
      t.Errorf("Expected output to contain standalone MubiId example (shortened to 'M'). Got:\n%s", output)
   }

   // 2. Test that dependent examples generated correctly
   // Notice 'A' is shortened, but 'Season' is full because Session/SetProxy start with S
   expectedSeasonExample := "\tmyapp A xyz Season 123\n"
   if !strings.Contains(output, expectedSeasonExample) {
      t.Errorf("Expected output to contain dependent Season example '%s'. Got:\n%s", expectedSeasonExample, output)
   }

   // Notice both 'M' and 'U' are safely shortened
   expectedProxyExample := "\tmyapp M 123 U\n"
   if !strings.Contains(output, expectedProxyExample) {
      t.Errorf("Expected output to contain dependent UseProxy example '%s'. Got:\n%s", expectedProxyExample, output)
   }

   // 3. Test that standalone bools generated correctly (shortened to L)
   if !strings.Contains(output, "\tmyapp L\n") {
      t.Errorf("Expected output to contain standalone LinkCode example (shortened to 'L'). Got:\n%s", output)
   }
}

func TestParseFlags(t *testing.T) {
   var c client

   // Testing that our normal parsing still works, including partial matches
   args := []string{"Addr", "https://test.com", "Sea", "5", "Use"}

   err := ParseFlags(args, &c)
   if err != nil {
      t.Fatalf("ParseFlags returned an unexpected error: %v", err)
   }

   // Check string parsing
   if !c.Address.Set || c.Address.Value != "https://test.com" {
      t.Errorf("Expected Address to be set to 'https://test.com', got Set:%v Value:%v", c.Address.Set, c.Address.Value)
   }

   // Check int parsing
   if !c.Season.Set || c.Season.Value != 5 {
      t.Errorf("Expected Season to be set to 5, got Set:%v Value:%v", c.Season.Set, c.Season.Value)
   }

   // Check bool parsing
   if !c.UseProxy.Set || c.UseProxy.Value != true {
      t.Errorf("Expected UseProxy to be true, got Set:%v Value:%v", c.UseProxy.Set, c.UseProxy.Value)
   }

   // Check that uncalled flags remain unset
   if c.MubiId.Set {
      t.Errorf("Expected MubiId to remain unset")
   }
}

func TestFormatFlags_Errors(t *testing.T) {
   var buf bytes.Buffer

   // Passing a value instead of a pointer should fail
   err := FormatFlags(&buf, "myapp", client{})
   if err == nil {
      t.Errorf("Expected an error when passing a non-pointer to FormatFlags")
   }

   // Passing a pointer to a non-struct should fail
   num := 5
   err = FormatFlags(&buf, "myapp", &num)
   if err == nil {
      t.Errorf("Expected an error when passing a pointer to a non-struct")
   }
}

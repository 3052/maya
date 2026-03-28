package maya

import "testing"

func TestPrintFlags(t *testing.T) {
   flags = nil // reset global state

   s := "hello"
   f1 := StringFlag(&s, "s", "string flag")

   f2 := BoolFlag("b", "bool flag")

   i := 42
   f3 := IntFlag(&i, "i", "int flag")

   err := PrintFlags([][]*Flag{
      {f1, f2},
      {f3},
   })
   if err != nil {
      t.Fatalf("expected nil error, got: %v", err)
   }
}

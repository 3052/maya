package maya

import (
   "fmt"
   "testing"
)

func TestFlagSet_String(t *testing.T) {
   fmt.Println("--- Multiple Groups ---")
   var fsMulti FlagSet

   fsMulti.Add(1, "help-menu")
   fsMulti.Add(1, "verbose-logging-output")

   fsMulti.AddValue(2, "database-username")
   fsMulti.AddValue(2, "database-password")

   fmt.Println(fsMulti.String())

   fmt.Println("\n--- Single Group ---")
   var fsSingle FlagSet

   fsSingle.Add(1, "help-menu")
   fsSingle.AddValue(1, "target-url")

   fmt.Println(fsSingle.String())
}

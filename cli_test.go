package maya

import (
   "fmt"
   "testing"
)

func TestFlagSet_String(t *testing.T) {
   fmt.Println("--- Multiple Groups ---")
   var fsMulti FlagSet

   fsMulti.AddGroup("help-menu", false, 1)
   fsMulti.AddGroup("verbose-logging-output", false, 1)

   fsMulti.AddGroup("database-username", true, 2)
   fsMulti.AddGroup("database-password", true, 2)

   fmt.Println(fsMulti.String())

   fmt.Println("\n--- Single Group ---")
   var fsSingle FlagSet

   fsSingle.Add("help-menu", false)
   fsSingle.Add("target-url", true)

   fmt.Println(fsSingle.String())
}

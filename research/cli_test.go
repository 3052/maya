package cli

import (
   "fmt"
   "testing"
)

func TestFlagSet_String(t *testing.T) {
   var fs FlagSet

   fs.Add(1, false, "show the help menu")
   fs.Add(1, false, "enable verbose logging output")

   fs.Add(2, true, "provide the database username")
   fs.Add(2, true, "provide the database password")

   fmt.Println(fs.String())
}

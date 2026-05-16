package maya

import (
   "fmt"
   "testing"
)

func TestPrintFlagSetString(t *testing.T) {
   var fs FlagSet
   var fDebug, fFile Flag

   fs.Add(&fDebug, "enable debug mode")
   fs.AddValue(&fFile, "set output file")

   fmt.Println(fs.String())
}

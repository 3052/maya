package maya

import (
   "os"
   "testing"
)

func TestFormatFlags_ConsoleOutput(t *testing.T) {
   type AppConfig struct {
      Verbose Flag[bool]   `usage:"Enable verbose logging"`
      Port    Flag[int]    `usage:"Port to listen on"`
      Host    Flag[string] `usage:"Host address to bind" depends:"Port"`
   }

   var cfg AppConfig

   // Output directly to the console
   err := FormatFlags(os.Stdout, "myapp", &cfg)
   if err != nil {
      t.Fatalf("FormatFlags failed: %v", err)
   }
}

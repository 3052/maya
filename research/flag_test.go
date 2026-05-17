package myflag

import (
   "os"
   "testing"
)

func TestParse(t *testing.T) {
   originalArgs := os.Args
   defer func() { os.Args = originalArgs }()

   type Config struct {
      Verbose Flag
      Name    Flag `flag:"value" group:"User"`
      Port    Flag `flag:"value" group:"Server"`
      Ignored string
   }

   // 1. Valid flags
   os.Args = []string{"app", "Verbose", "Name", "John", "Port", "8080"}
   var cfg Config

   if err := Parse(&cfg); err != nil {
      t.Fatalf("expected successful parse, got err: %v", err)
   }

   if !cfg.Verbose.Set || !cfg.Name.Set || cfg.Name.Value != "John" || cfg.Port.Value != "8080" {
      t.Fatalf("parse state incorrect: %+v", cfg)
   }

   // Assert that groups were populated
   if cfg.Name.Group != "User" || cfg.Port.Group != "Server" {
      t.Fatalf("groups parsed incorrectly. Name group: %s, Port group: %s", cfg.Name.Group, cfg.Port.Group)
   }
}

func TestUsage(t *testing.T) {
   originalArgs := os.Args
   defer func() { os.Args = originalArgs }()

   type Config struct {
      Verbose Flag
      Name    Flag `flag:"value" group:"User Options"`
      Age     Flag `flag:"value" group:"User Options"`
      Host    Flag `flag:"value" group:"Server Settings"`
      Port    Flag `flag:"value" group:"Server Settings"`
   }

   // Mock the program name for the usage output
   os.Args = []string{"my_awesome_app"}
   var cfg Config

   // Output a newline to make the console output cleaner during go test
   os.Stderr.WriteString("\n--- USAGE OUTPUT ---\n")

   // This will print directly to the console (os.Stderr)
   Usage(&cfg)

   os.Stderr.WriteString("--------------------\n\n")
}

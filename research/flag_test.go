package myflag

import (
   "os"
   "testing"
)

func TestParse(t *testing.T) {
   originalArgs := os.Args
   defer func() { os.Args = originalArgs }()

   type Config struct {
      VerboseOutput Flag
      HelloWorld    Flag `value:"true" group:"User"`
      ServerPort    Flag `value:"true" group:"Server"`
   }

   // 1. Valid flags using partial matches
   // "Verbose"   -> matches "VerboseOutput"
   // "World"     -> matches "HelloWorld"
   // "ServerPort"-> exact match (bypasses ambiguity checks)
   os.Args = []string{"app", "Verbose", "World", "John", "ServerPort", "8080"}
   var cfg Config

   if err := Parse(&cfg); err != nil {
      t.Fatalf("expected successful parse, got err: %v", err)
   }

   if !cfg.VerboseOutput.Set || !cfg.HelloWorld.Set || cfg.HelloWorld.Value != "John" || cfg.ServerPort.Value != "8080" {
      t.Fatalf("parse state incorrect: %+v", cfg)
   }

   // Assert that groups were populated directly as strings
   if cfg.HelloWorld.Group != "User" || cfg.ServerPort.Group != "Server" {
      t.Fatalf("groups parsed incorrectly. HelloWorld group: %s, ServerPort group: %s", cfg.HelloWorld.Group, cfg.ServerPort.Group)
   }
}

func TestUsage_NoGroups(t *testing.T) {
   type Config struct {
      VerboseOutput Flag
      HelloWorld    Flag `value:"true"`
      ServerPort    Flag `value:"true"`
   }

   var cfg Config

   os.Stdout.WriteString("\n--- USAGE OUTPUT (NO GROUPS) ---\n")
   if err := Usage(&cfg); err != nil {
      t.Fatalf("expected no error from Usage, got: %v", err)
   }
   os.Stdout.WriteString("--------------------------------\n\n")
}

func TestUsage_AllGroups(t *testing.T) {
   type Config struct {
      HelloWorld  Flag `value:"true" group:"User Options"`
      HelloPlanet Flag `value:"true" group:"User Options"`
      ServerHost  Flag `value:"true" group:"Server Settings"`
      ServerPort  Flag `value:"true" group:"Server Settings"`
   }

   var cfg Config

   os.Stdout.WriteString("\n--- USAGE OUTPUT (ALL GROUPS) ---\n")
   if err := Usage(&cfg); err != nil {
      t.Fatalf("expected no error from Usage, got: %v", err)
   }
   os.Stdout.WriteString("---------------------------------\n\n")
}

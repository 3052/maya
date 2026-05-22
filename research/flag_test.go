package myflag

import (
   "reflect"
   "testing"
)

type Config struct {
   Verbose bool   `usage:"enable verbose output"`
   Name    string `usage:"user name"`
   Role    string `usage:"user role"`
}

func TestParse_Success(t *testing.T) {
   cfg := Config{
      Role: "guest", // Ensure default values are preserved if not overridden
   }

   args := []string{"Verbose", "Name", "admin"}

   err := Parse(&cfg, args)
   if err != nil {
      t.Fatalf("expected no error, got: %v", err)
   }

   if !cfg.Verbose {
      t.Errorf("expected Verbose to be true")
   }
   if cfg.Name != "admin" {
      t.Errorf("expected Name to be 'admin', got '%s'", cfg.Name)
   }
   if cfg.Role != "guest" {
      t.Errorf("expected Role to remain 'guest', got '%s'", cfg.Role)
   }
}

func TestParse_MissingArgument(t *testing.T) {
   cfg := Config{}
   args := []string{"Name"}

   err := Parse(&cfg, args)
   if err == nil {
      t.Fatalf("expected error for missing argument, got nil")
   }

   expectedErr := "flag needs an argument: Name"
   if err.Error() != expectedErr {
      t.Errorf("expected error '%s', got '%s'", expectedErr, err.Error())
   }
}

func TestParse_UndefinedFlag(t *testing.T) {
   cfg := Config{}
   args := []string{"UnknownFlag"}

   err := Parse(&cfg, args)
   if err == nil {
      t.Fatalf("expected error for undefined flag, got nil")
   }

   expectedErr := "flag provided but not defined: UnknownFlag"
   if err.Error() != expectedErr {
      t.Errorf("expected error '%s', got '%s'", expectedErr, err.Error())
   }
}

func TestParse_InvalidTargetTypes(t *testing.T) {
   tests := []struct {
      name   string
      target any
   }{
      {"nil target", nil},
      {"non-pointer struct", Config{}},
      {"pointer to non-struct", reflect.ValueOf("string").Interface()},
   }

   for _, tt := range tests {
      t.Run(tt.name, func(t *testing.T) {
         err := Parse(tt.target, []string{})
         if err == nil {
            t.Fatalf("expected error for invalid target, got nil")
         }
         expectedErr := "target must be a pointer to a struct"
         if err.Error() != expectedErr {
            t.Errorf("expected error '%s', got '%s'", expectedErr, err.Error())
         }
      })
   }
}

package flag

import (
   "fmt"
   "slices"
   "strconv"
   "strings"
)

// ==========================================
// Core Types
// ==========================================

// A Flag represents the state of a single command line flag.
type Flag struct {
   Name   string
   Usage  string
   Value  string // Parsed value stored directly as a string
   IsBool bool   // True if the flag doesn't require a subsequent argument
}

// A FlagSet represents a set of defined flags.
type FlagSet []*Flag

// ==========================================
// Flag Registration
// ==========================================

// String adds a new string flag to the FlagSet.
func (f *FlagSet) String(name string, value string, usage string) {
   *f = append(*f, &Flag{
      Name:   name,
      Usage:  usage,
      Value:  value,
      IsBool: false,
   })
}

// Bool adds a new boolean flag to the FlagSet.
func (f *FlagSet) Bool(name string, value bool, usage string) {
   *f = append(*f, &Flag{
      Name:   name,
      Usage:  usage,
      Value:  strconv.FormatBool(value),
      IsBool: true,
   })
}

// ==========================================
// Parsing Logic
// ==========================================

// Parse parses flag definitions from the argument list.
func (f *FlagSet) Parse(arguments []string) error {
   args := arguments

   for {
      if len(args) == 0 {
         return nil
      }

      arg := args[0]

      // Stop parsing if it's not a flag (doesn't start with "-")
      if !strings.HasPrefix(arg, "-") || arg == "-" {
         return nil
      }

      // Pop the argument from the list
      args = args[1:]

      // Strip leading dashes
      trimmed := strings.TrimLeft(arg, "-")

      // Check for key=value format
      parts := strings.SplitN(trimmed, "=", 2)
      name := parts[0]

      // Find the flag in the slice
      idx := slices.IndexFunc(*f, func(fl *Flag) bool {
         return fl.Name == name
      })

      if idx == -1 {
         return fmt.Errorf("flag provided but not defined: -%s", name)
      }

      flag := (*f)[idx]

      var value string
      if len(parts) == 2 {
         // Format: -flag=value
         value = parts[1]
      } else {
         // Format: -flag value OR boolean -flag
         if flag.IsBool {
            value = "true" // Booleans don't consume the next argument
         } else {
            // Requires next argument
            if len(args) == 0 {
               return fmt.Errorf("flag needs an argument: -%s", name)
            }
            value = args[0]
            args = args[1:] // Consume the argument
         }
      }

      // Store the parsed result directly as a string
      flag.Value = value
   }
}

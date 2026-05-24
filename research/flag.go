package flag

import (
   "fmt"
   "strings"
)

// Flag represents the state of a single string flag.
type Flag struct {
   Name     string
   Usage    string
   Value    *string
   DefValue string
}

// FlagSet represents a set of defined string flags.
type FlagSet []*Flag

// Lookup returns the Flag structure of the named flag, returning nil if none exists.
func (f *FlagSet) Lookup(name string) *Flag {
   for _, flag := range *f {
      if flag.Name == name {
         return flag
      }
   }
   return nil
}

// StringVar defines a string flag with specified name, default value, and usage string.
// The argument p points to a string variable in which to store the value of the flag.
func (f *FlagSet) StringVar(p *string, name string, value string, usage string) {
   *p = value
   *f = append(*f, &Flag{
      Name:     name,
      Usage:    usage,
      Value:    p,
      DefValue: value,
   })
}

// Parse parses flag definitions from the argument list.
// It only supports "key", "key=", and "key=value" formats.
func (f *FlagSet) Parse(arguments []string) error {
   for _, s := range arguments {
      // strings.Cut perfectly handles all three scenarios:
      // "key=value" -> name: "key", value: "value"
      // "key="      -> name: "key", value: ""
      // "key"       -> name: "key", value: "" (when separator is not found)
      name, value, _ := strings.Cut(s, "=")

      // Find the flag using Lookup
      flag := f.Lookup(name)
      if flag == nil {
         return fmt.Errorf("flag provided but not defined: %s", name)
      }

      *flag.Value = value
   }
   return nil
}

// PrintDefaults prints a usage message showing the default settings of all defined flags.
func (f *FlagSet) PrintDefaults() {
   for _, flag := range *f {
      fmt.Printf("\t%s string\n\t\t%s (default %q)\n", flag.Name, flag.Usage, flag.DefValue)
   }
}

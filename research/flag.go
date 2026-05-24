package flag

import (
   "fmt"
   "strconv"
   "strings"
)

// Value is the interface to the dynamic value stored in a flag.
type Value interface {
   String() string
   Set(string) error
}

// StringValue contains a string flag's value and tracks if it has been set.
type StringValue struct {
   Value string
   IsSet bool
}

// String returns the string representation of the value.
func (s *StringValue) String() string {
   return s.Value
}

// Set parses the string and updates the value state.
func (s *StringValue) Set(input string) error {
   s.Value = input
   s.IsSet = true
   return nil
}

// IntValue contains an int flag's value and tracks if it has been set.
type IntValue struct {
   Value int
   IsSet bool
}

// String returns the string representation of the value.
func (i *IntValue) String() string {
   return strconv.Itoa(i.Value)
}

// Set parses the integer string and updates the value state.
func (i *IntValue) Set(input string) error {
   var err error
   i.Value, err = strconv.Atoi(input)
   if err != nil {
      return err
   }
   i.IsSet = true
   return nil
}

// Flag represents the state of a single flag.
type Flag struct {
   Name  string
   Usage string
   Value Value
}

// FlagSet represents a set of defined flags.
type FlagSet []*Flag

// Lookup returns the Flag structure of the named flag, returning nil if none exists.
func (fs FlagSet) Lookup(name string) *Flag {
   for _, f := range fs {
      if f.Name == name {
         return f
      }
   }
   return nil
}

// Parse parses flag definitions from the argument list.
// It only supports "key", "key=", and "key=value" formats.
func (fs FlagSet) Parse(arguments []string) error {
   for _, s := range arguments {
      // strings.Cut perfectly handles all three scenarios:
      // "key=value" -> name: "key", value: "value"
      // "key="      -> name: "key", value: ""
      // "key"       -> name: "key", value: "" (when separator is not found)
      name, value, _ := strings.Cut(s, "=")

      // Find the flag using Lookup
      f := fs.Lookup(name)
      if f == nil {
         return fmt.Errorf("flag provided but not defined: %s", name)
      }

      // Delegate the parsing and state updating to the interface method
      if err := f.Value.Set(value); err != nil {
         return fmt.Errorf("invalid value %q for flag %s: %v", value, name, err)
      }
   }
   return nil
}

// PrintDefaults prints a usage message showing all defined flags.
func (fs FlagSet) PrintDefaults() {
   for _, f := range fs {
      fmt.Printf("%s value\n\t%s (default %s)\n", f.Name, f.Usage, f.Value.String())
   }
}

package flag

import (
   "errors"
   "fmt"
   "os"
   "strconv"
   "strings"
)

// ErrHelp is the error returned if the -help or -h flag is invoked
// but no such flag is defined.
var ErrHelp = errors.New("flag: help requested")

// ==========================================
// Interfaces
// ==========================================

// Value is the interface to the dynamic value stored in a flag.
type Value interface {
   String() string
   Set(string) error
}

// boolFlag is an optional interface to indicate a flag is a boolean.
// This allows the parser to know it doesn't require a subsequent argument.
type boolFlag interface {
   Value
   IsBoolFlag() bool
}

// ==========================================
// Core Types
// ==========================================

// A Flag represents the state of a single command line flag.
type Flag struct {
   Name     string
   Usage    string
   Value    Value
   DefValue string
}

// A FlagSet represents a set of defined flags.
type FlagSet struct {
   name   string
   parsed bool
   flags  map[string]*Flag
   args   []string // remaining non-flag arguments
}

// NewFlagSet returns a new, empty flag set with the specified name.
func NewFlagSet(name string) *FlagSet {
   return &FlagSet{
      name:  name,
      flags: make(map[string]*Flag),
   }
}

// ==========================================
// Value Implementations & Binders
// ==========================================

// --- String ---
type stringValue string

func (s *stringValue) Set(val string) error {
   *s = stringValue(val)
   return nil
}
func (s *stringValue) String() string { return string(*s) }

func (f *FlagSet) StringVar(p *string, name string, value string, usage string) {
   *p = value // Set default
   f.flags[name] = &Flag{Name: name, Usage: usage, Value: (*stringValue)(p), DefValue: value}
}

// --- Int ---
type intValue int

func (i *intValue) Set(val string) error {
   v, err := strconv.Atoi(val)
   if err != nil {
      return err
   }
   *i = intValue(v)
   return nil
}
func (i *intValue) String() string { return strconv.Itoa(int(*i)) }

func (f *FlagSet) IntVar(p *int, name string, value int, usage string) {
   *p = value
   f.flags[name] = &Flag{Name: name, Usage: usage, Value: (*intValue)(p), DefValue: strconv.Itoa(value)}
}

// --- Bool ---
type boolValue bool

func (b *boolValue) Set(val string) error {
   v, err := strconv.ParseBool(val)
   if err != nil {
      return err
   }
   *b = boolValue(v)
   return nil
}
func (b *boolValue) String() string { return strconv.FormatBool(bool(*b)) }
func (b *boolValue) IsBoolFlag() bool { return true }

func (f *FlagSet) BoolVar(p *bool, name string, value bool, usage string) {
   *p = value
   f.flags[name] = &Flag{Name: name, Usage: usage, Value: (*boolValue)(p), DefValue: strconv.FormatBool(value)}
}

// ==========================================
// Parsing Logic
// ==========================================

// Parse parses flag definitions from the argument list, which should not
// include the command name.
func (f *FlagSet) Parse(arguments []string) error {
   f.parsed = true
   f.args = arguments

   for {
      if len(f.args) == 0 {
         return nil
      }

      arg := f.args[0]

      // Stop parsing if we hit "--"
      if arg == "--" {
         f.args = f.args[1:]
         return nil
      }

      // Stop parsing if it's not a flag (doesn't start with "-")
      if !strings.HasPrefix(arg, "-") || arg == "-" {
         return nil
      }

      // Pop the argument from the list
      f.args = f.args[1:]

      // Strip leading dashes
      trimmed := strings.TrimLeft(arg, "-")

      // Handle built-in help
      if trimmed == "help" || trimmed == "h" {
         if _, exists := f.flags[trimmed]; !exists {
            return ErrHelp
         }
      }

      // Check for key=value format
      parts := strings.SplitN(trimmed, "=", 2)
      name := parts[0]

      flag, exists := f.flags[name]
      if !exists {
         return fmt.Errorf("flag provided but not defined: -%s", name)
      }

      var value string
      if len(parts) == 2 {
         // Format: -flag=value
         value = parts[1]
      } else {
         // Format: -flag value OR boolean -flag
         if bFlag, ok := flag.Value.(boolFlag); ok && bFlag.IsBoolFlag() {
            value = "true" // Booleans don't consume the next argument
         } else {
            // Requires next argument
            if len(f.args) == 0 {
               return fmt.Errorf("flag needs an argument: -%s", name)
            }
            value = f.args[0]
            f.args = f.args[1:] // Consume the argument
         }
      }

      if err := flag.Value.Set(value); err != nil {
         return fmt.Errorf("invalid value %q for flag -%s: %v", value, name, err)
      }
   }
}

// Parsed reports whether f.Parse has been called.
func (f *FlagSet) Parsed() bool {
   return f.parsed
}

// Args returns the non-flag arguments.
func (f *FlagSet) Args() []string {
   return f.args
}

// PrintDefaults prints, to standard error, the default values of all defined flags.
func (f *FlagSet) PrintDefaults() {
   fmt.Fprintf(os.Stderr, "Usage of %s:\n", f.name)
   for _, flag := range f.flags {
      fmt.Fprintf(os.Stderr, "  -%s\n    \t%s (default: %s)\n", flag.Name, flag.Usage, flag.DefValue)
   }
}

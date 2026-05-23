// maya.go
package maya

import (
   "fmt"
   "strings"
)

type Flag struct {
   Name       string
   Value      string
   Usage      string
   Set        bool
   NeedsValue bool
   Needs      *Flag
}

func (f *Flag) SetName(name string) *Flag {
   f.Name = name
   return f
}

func (f *Flag) SetValue(value string) *Flag {
   f.Value = value
   return f
}

func (f *Flag) SetUsage(usage string) *Flag {
   f.Usage = usage
   return f
}

func (f *Flag) SetNeedsValue(needsValue bool) *Flag {
   f.NeedsValue = needsValue
   return f
}

func (f *Flag) SetNeeds(other *Flag) *Flag {
   f.Needs = other
   return f
}

// ParseFlag parses the flag's underlying string value into type T using the provided parsing function.
func ParseFlag[T any](f *Flag, parse func(string) (T, error)) (T, error) {
   return parse(f.Value)
}

type FlagSet []*Flag

func (fs FlagSet) Parse(args []string) error {
   for _, arg := range args {
      name, value, hasEqual := strings.Cut(arg, "=")

      var match *Flag
      var matches int

      for _, f := range fs {
         if strings.HasPrefix(f.Name, name) {
            match = f
            matches++
         }
      }

      if matches > 1 {
         return fmt.Errorf("ambiguous argument: %s", name)
      }
      if matches == 0 {
         return fmt.Errorf("unknown argument: %s", name)
      }

      if match.NeedsValue && !hasEqual {
         return fmt.Errorf("argument requires a value: %s", name)
      }
      if !match.NeedsValue && hasEqual {
         return fmt.Errorf("argument does not take a value: %s", name)
      }

      match.Value = value
      match.Set = true
   }

   return nil
}

func (fs FlagSet) Usage() string {
   data := new(strings.Builder)
   for _, f := range fs {
      if f.NeedsValue {
         fmt.Fprintf(data, "%s value\n", f.Name)
      } else {
         fmt.Fprintf(data, "%s\n", f.Name)
      }

      if f.Usage != "" && f.Value != "" {
         fmt.Fprintf(data, "\t%s (default: %s)\n", f.Usage, f.Value)
      } else if f.Usage != "" {
         fmt.Fprintf(data, "\t%s\n", f.Usage)
      } else if f.Value != "" {
         fmt.Fprintf(data, "\t(default: %s)\n", f.Value)
      }
   }
   return data.String()
}

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

type FlagSet []*Flag

func (fs FlagSet) Parse(args []string) error {
   for _, f := range fs {
      if f.Name == "" {
         return fmt.Errorf("flag name cannot be empty")
      }
   }

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

func (fs FlagSet) Usage() (string, error) {
   for _, f := range fs {
      if f.Name == "" {
         return "", fmt.Errorf("flag name cannot be empty")
      }
   }

   data := new(strings.Builder)

   // Helper to determine if we should use the 1-letter prefix or full name
   getExampleName := func(f *Flag) string {
      first := f.Name[:1]
      count := 0
      for _, fl := range fs {
         if strings.HasPrefix(fl.Name, first) {
            count++
         }
      }
      if count == 1 {
         return first
      }
      return f.Name
   }

   // Helper to build the "Name=value" or "Name" string for the example
   buildPart := func(f *Flag) string {
      name := getExampleName(f)
      if f.NeedsValue {
         return name + "=value"
      }
      return name
   }

   fmt.Fprint(data, "Index:\n")
   for _, f := range fs {
      if f.NeedsValue {
         fmt.Fprintf(data, "\t%s value\n", f.Name)
      } else {
         fmt.Fprintf(data, "\t%s\n", f.Name)
      }

      if f.Usage != "" && f.Value != "" {
         fmt.Fprintf(data, "\t\t%s (default: %s)\n", f.Usage, f.Value)
      } else if f.Usage != "" {
         fmt.Fprintf(data, "\t\t%s\n", f.Usage)
      } else if f.Value != "" {
         fmt.Fprintf(data, "\t\t(default: %s)\n", f.Value)
      }
   }

   fmt.Fprint(data, "\nExamples:\n")
   for _, f := range fs {
      if f.Needs != nil {
         fmt.Fprintf(data, "\tapp %s %s\n", buildPart(f.Needs), buildPart(f))
      } else {
         fmt.Fprintf(data, "\tapp %s\n", buildPart(f))
      }
   }

   return data.String(), nil
}

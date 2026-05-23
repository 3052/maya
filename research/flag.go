// maya.go
package maya

import (
   "fmt"
   "strings"
)

type Flag struct {
   Name     string
   Value    string
   Usage    string
   Set      bool
   HasEqual bool
   Requires *Flag
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

func (f *Flag) SetRequires(other *Flag) *Flag {
   f.Requires = other
   return f
}

type FlagSet []*Flag

func (fs FlagSet) Lookup(name string) *Flag {
   for _, f := range fs {
      if f.Name == name {
         return f
      }
   }
   return nil
}

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

      match.Value = value
      match.Set = true
      match.HasEqual = hasEqual
   }

   return nil
}

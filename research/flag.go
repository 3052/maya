package maya

import (
   "fmt"
   "strings"
)

type Flag struct {
   Name  string
   Value string
}

func (f *Flag) Define(name string, value string) *Flag {
   f.Name = name
   f.Value = value
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
   for i := 0; i < len(args); i++ {
      name, value, _ := strings.Cut(args[i], "=")

      flag := fs.Lookup(name)
      if flag == nil {
         return fmt.Errorf("unknown argument: %s", name)
      }

      flag.Value = value
   }
   return nil
}

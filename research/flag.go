package myflag

import (
   "fmt"
   "os"
   "slices"
   "strconv"
)

type Flag struct {
   Name     string
   Usage    string
   IsBool   bool
   Provided bool
   Set      func(string) error
}

var flags []*Flag

func String(name, usage string) (*Flag, *string) {
   p := new(string)
   f := &Flag{
      Name:  name,
      Usage: usage,
      Set: func(val string) error {
         *p = val
         return nil
      },
   }
   flags = append(flags, f)
   return f, p
}

func Bool(name, usage string) (*Flag, *bool) {
   p := new(bool)
   f := &Flag{
      Name:   name,
      Usage:  usage,
      IsBool: true,
      Set: func(val string) error {
         if val == "true" {
            *p = true
         }
         return nil
      },
   }
   flags = append(flags, f)
   return f, p
}

func Int(name, usage string) (*Flag, *int) {
   p := new(int)
   f := &Flag{
      Name:  name,
      Usage: usage,
      Set: func(val string) (err error) {
         *p, err = strconv.Atoi(val)
         return
      },
   }
   flags = append(flags, f)
   return f, p
}

func Parse() error {
   args := os.Args[1:]

   for i := 0; i < len(args); i++ {
      arg := args[i]

      if len(arg) < 2 || arg[0] != '-' {
         break
      }

      name := arg[1:]

      idx := slices.IndexFunc(flags, func(f *Flag) bool {
         return f.Name == name
      })

      if idx == -1 {
         return fmt.Errorf("provided but not defined: -%s", name)
      }
      flag := flags[idx]

      var value string

      if flag.IsBool {
         value = "true"
      } else {
         if i+1 >= len(args) || (len(args[i+1]) >= 2 && args[i+1][0] == '-') {
            return fmt.Errorf("flag needs an argument: -%s", name)
         }
         value = args[i+1]
         i++
      }

      if err := flag.Set(value); err != nil {
         return fmt.Errorf("invalid value for flag -%s: %v", name, err)
      }

      flag.Provided = true
   }
   return nil
}

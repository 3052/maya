// cli.go
package maya

import (
   "encoding/xml"
   "fmt"
   "log"
   "os"
   "path/filepath"
   "slices"
   "strconv"
)

type Flag struct {
   Name   string
   IsBool bool
   IsSet  bool
   Set    func(string) error
   Usage  string
}

var flags []*Flag

func FuncFlag(name, usage string, fn func(string) error) *Flag {
   option := &Flag{
      Name:  name,
      Set:   fn,
      Usage: fmt.Sprintf(" value\n\t%s", usage),
   }

   flags = append(flags, option)
   return option
}

func StringFlag(pointer *string, name, usage string) *Flag {
   usage = fmt.Sprintf(" string\n\t%s", usage)
   if *pointer != "" {
      usage += fmt.Sprintf("\n\tdefault %s", *pointer)
   }

   option := &Flag{
      Name: name,
      Set: func(val string) error {
         *pointer = val
         return nil
      },
      Usage: usage,
   }

   flags = append(flags, option)
   return option
}

func BoolFlag(name, usage string) *Flag {
   option := &Flag{
      Name:   name,
      IsBool: true,
      Usage:  fmt.Sprintf("\n\t%s", usage),
   }

   flags = append(flags, option)
   return option
}

func IntFlag(pointer *int, name, usage string) *Flag {
   usage = fmt.Sprintf(" int\n\t%s", usage)
   if *pointer != 0 {
      usage += fmt.Sprintf("\n\tdefault %d", *pointer)
   }

   option := &Flag{
      Name: name,
      Set: func(val string) (err error) {
         *pointer, err = strconv.Atoi(val)
         return
      },
      Usage: usage,
   }

   flags = append(flags, option)
   return option
}

func ParseFlags() error {
   for index := 1; index < len(os.Args); index++ {
      arg := os.Args[index]

      if len(arg) < 2 || arg[0] != '-' {
         return fmt.Errorf("unexpected argument: %s", arg)
      }

      name := arg[1:]

      idx := slices.IndexFunc(flags, func(option *Flag) bool {
         return option.Name == name
      })

      if idx == -1 {
         return fmt.Errorf("provided but not defined: -%s", name)
      }
      option := flags[idx]

      if !option.IsBool {
         index++
         if index >= len(os.Args) {
            return fmt.Errorf("flag needs an argument: -%s", name)
         }

         if err := option.Set(os.Args[index]); err != nil {
            return fmt.Errorf("invalid value for flag -%s: %v", name, err)
         }
      }

      option.IsSet = true

   }

   return nil
}

func PrintFlags(groups [][]*Flag) error {
   printed := make(map[*Flag]bool)

   for index, group := range groups {
      if index > 0 {
         fmt.Fprintln(os.Stderr)
      }

      for _, option := range group {
         fmt.Fprintf(os.Stderr, "-%s%s\n", option.Name, option.Usage)
         printed[option] = true
      }

   }

   for _, option := range flags {
      if !printed[option] {
         return fmt.Errorf("flag -%s is missing from PrintFlags groups", option.Name)
      }

   }
   return nil
}

func (c *Cache) Read(value any) func(func() error) error {
   data, err := os.ReadFile(c.File)
   if err == nil {
      err = xml.Unmarshal(data, value)
   }

   return func(action func() error) error {
      if err != nil {
         return err
      }

      return action()
   }

}

type Cache struct {
   File string
}

func (c *Cache) Setup(file string) error {
   var err error
   c.File, err = os.UserCacheDir()
   if err != nil {
      return err
   }

   c.File = filepath.Join(c.File, file)
   return os.MkdirAll(filepath.Dir(c.File), os.ModePerm)
}

func (c *Cache) Write(value any) error {
   data, err := xml.MarshalIndent(value, "", " ")
   if err != nil {
      return err
   }

   log.Println("Write", c.File)
   return os.WriteFile(c.File, data, os.ModePerm)
}

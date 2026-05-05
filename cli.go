// cli.go
package maya

import (
   "encoding/xml"
   "fmt"
   "log"
   "os"
   "path/filepath"
   "reflect"
   "slices"
   "strconv"
)

// Cache holds the pre-computed OS path for the cache directory.
type Cache struct {
   FullPath string
}

// Setup computes the full cache path, creates the directory exactly once,
// and stores the path in the Cache struct.
func (c *Cache) Setup(dirName string) error {
   cacheDir, err := os.UserCacheDir()
   if err != nil {
      return fmt.Errorf("failed to get cache directory: %w", err)
   }

   c.FullPath = filepath.Join(cacheDir, dirName)

   // Create the directory immediately upon setup
   if err := os.MkdirAll(c.FullPath, os.ModePerm); err != nil {
      return fmt.Errorf("failed to create directory: %w", err)
   }

   return nil
}

// GetFilePath unwraps pointers and builds the absolute string path for the file.
// Exported so users can manually locate, check, or delete cache files.
func (c *Cache) GetFilePath(v any) string {
   t := reflect.TypeOf(v)
   for t.Kind() == reflect.Ptr {
      t = t.Elem()
   }

   return filepath.Join(c.FullPath, t.Name()+".xml")
}

// Encode marshals the value and writes it to the cache directory.
// It logs the full path immediately before attempting to write the file.
func (c *Cache) Encode(v any) error {
   filename := c.GetFilePath(v)

   data, err := xml.MarshalIndent(v, "", "  ")
   if err != nil {
      return fmt.Errorf("failed to encode XML: %w", err)
   }

   log.Println("Saving:", filename)

   err = os.WriteFile(filename, data, os.ModePerm)
   if err != nil {
      return fmt.Errorf("failed to write file: %w", err)
   }

   return nil
}

// Decode reads the XML from the cache directory and populates the struct.
func (c *Cache) Decode(v any) error {
   filename := c.GetFilePath(v)

   data, err := os.ReadFile(filename)
   if err != nil {
      return fmt.Errorf("failed to read file: %w", err)
   }

   err = xml.Unmarshal(data, v)
   if err != nil {
      return fmt.Errorf("failed to decode XML: %w", err)
   }

   return nil
}

// Update reads the existing XML into v, calls your modify function,
// and writes the updated value back to disk.
// If the callback returns an error, the write is aborted.
func (c *Cache) Update(v any, modify func() error) error {
   // 1. Read existing data
   if err := c.Decode(v); err != nil {
      return fmt.Errorf("failed to decode for update: %w", err)
   }

   // 2. Modify the data using the provided callback
   if err := modify(); err != nil {
      return fmt.Errorf("update aborted by callback: %w", err)
   }

   // 3. Save it back
   if err := c.Encode(v); err != nil {
      return fmt.Errorf("failed to encode after update: %w", err)
   }

   return nil
}

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

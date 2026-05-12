// cli.go
package maya

import (
   "encoding/xml"
   "fmt"
   "log"
   "net/url"
   "os"
   "path/filepath"
   "reflect"
   "strconv"
   "strings"
)

// Decode reads the XML from the cache directory and populates the structs.
// It stops and returns an error on the first failure.
func (c *Cache) Decode(values ...any) error {
   for _, v := range values {
      filename := c.GetFilePath(v)
      data, err := os.ReadFile(filename)
      if err != nil {
         return err
      }
      err = xml.Unmarshal(data, v)
      if err != nil {
         return fmt.Errorf("failed to decode XML for %T: %w", v, err)
      }
   }
   return nil
}

// Cache holds the pre-computed OS path for the cache directory.
type Cache struct {
   FullPath string
}

// Encode marshals the values and writes them to the cache directory.
// It stops and returns an error on the first failure.
func (c *Cache) Encode(values ...any) error {
   for _, v := range values {
      filename := c.GetFilePath(v)

      data, err := xml.MarshalIndent(v, "", "  ")
      if err != nil {
         // Added type info to the error to know WHICH item failed
         return fmt.Errorf("failed to encode XML for %T: %w", v, err)
      }

      log.Println("create:", filename)

      err = os.WriteFile(filename, data, os.ModePerm)
      if err != nil {
         return fmt.Errorf("failed to write file %s: %w", filename, err)
      }
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

// Flag holds the metadata and state.
// AddValue flags populate Value. Add flags only toggle Set to true.
type Flag struct {
   Name     string
   Usage    string
   HasValue bool
   Value    string
   Set      bool
}

// String returns the formatted help text for the flag.
func (flag *Flag) String() string {
   if flag.HasValue {
      return fmt.Sprintf("-%s value\n\t%s", flag.Name, flag.Usage)
   }
   return fmt.Sprintf("-%s\n\t%s", flag.Name, flag.Usage)
}

// ParseInt parses the value as an int.
func (flag *Flag) ParseInt() (int, error) {
   parsed, err := strconv.Atoi(flag.Value)
   if err != nil {
      return 0, fmt.Errorf("invalid value %q for flag -%s", flag.Value, flag.Name)
   }
   return parsed, nil
}

// ParseUrl parses the value as a *url.URL.
func (flag *Flag) ParseUrl() (*url.URL, error) {
   parsed, err := url.Parse(flag.Value)
   if err != nil {
      return nil, fmt.Errorf("invalid value %q for flag -%s", flag.Value, flag.Name)
   }
   return parsed, nil
}

// FlagSet is a flat collection of defined flags.
type FlagSet []*Flag

// AddValue registers a flag that requires a value.
func (set *FlagSet) AddValue(name string, usage string) *Flag {
   flag := &Flag{
      Name:     name,
      Usage:    usage,
      HasValue: true,
   }
   *set = append(*set, flag)
   return flag
}

// Add registers a switch flag that does not take a value.
func (set *FlagSet) Add(name string, usage string) *Flag {
   flag := &Flag{
      Name:  name,
      Usage: usage,
   }
   *set = append(*set, flag)
   return flag
}

// Lookup returns the Flag with the specified name from the provided FlagSets, or nil if not found.
func Lookup(name string, sets ...FlagSet) *Flag {
   for _, set := range sets {
      for _, flag := range set {
         if flag.Name == name {
            return flag
         }
      }
   }
   return nil
}

// String returns the formatted help text for this specific set of flags.
func (set FlagSet) String() string {
   data := &strings.Builder{}
   for index, flag := range set {
      if index > 0 {
         fmt.Fprintln(data)
      }
      fmt.Fprint(data, flag)
   }
   return data.String()
}

// Parse loops through os.Args directly and checks against all provided FlagSets.
func Parse(sets ...FlagSet) error {
   for index := 1; index < len(os.Args); index++ {
      arg := os.Args[index]

      if len(arg) < 2 || arg[0] != '-' {
         return fmt.Errorf("unexpected argument or invalid flag format: %s", arg)
      }

      parsedName := arg[1:]
      found := Lookup(parsedName, sets...)

      if found == nil {
         return fmt.Errorf("flag provided but not defined: %s", arg)
      }

      found.Set = true

      if found.HasValue {
         if index+1 < len(os.Args) {
            found.Value = os.Args[index+1]
            index++ // consume the value so it isn't processed as a flag in the next iteration
         } else {
            return fmt.Errorf("flag '-%s' requires a value", found.Name)
         }
      }
   }
   return nil
}

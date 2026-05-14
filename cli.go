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
// It safely handles nil flags by returning an empty string (useful for spacing).
func (f *Flag) String() string {
   if f == nil {
      return ""
   }
   if f.HasValue {
      return fmt.Sprintf("-%s value\n\t%s", f.Name, f.Usage)
   }
   return fmt.Sprintf("-%s\n\t%s", f.Name, f.Usage)
}

// ParseInt parses the value as an int.
func (f *Flag) ParseInt() (int, error) {
   parsed, err := strconv.Atoi(f.Value)
   if err != nil {
      return 0, fmt.Errorf("invalid value %q for flag -%s", f.Value, f.Name)
   }
   return parsed, nil
}

// ParseUrl parses the value as a *url.URL.
func (f *Flag) ParseUrl() (*url.URL, error) {
   parsed, err := url.Parse(f.Value)
   if err != nil {
      return nil, fmt.Errorf("invalid value %q for flag -%s", f.Value, f.Name)
   }
   return parsed, nil
}

// FlagSet is a flat collection of defined flags.
// You can append nil to this slice to create visual line breaks in the output.
type FlagSet []*Flag

// AddValue registers a flag that requires a value.
func (fs *FlagSet) AddValue(f *Flag, name string, usage string) {
   f.Name = name
   f.Usage = usage
   f.HasValue = true
   *fs = append(*fs, f)
}

// Add registers a switch flag that does not take a value.
func (fs *FlagSet) Add(f *Flag, name string, usage string) {
   f.Name = name
   f.Usage = usage
   f.HasValue = false
   *fs = append(*fs, f)
}

// Lookup returns the Flag with the specified name, or nil if not found.
func (fs FlagSet) Lookup(name string) *Flag {
   for _, f := range fs {
      if f != nil && f.Name == name {
         return f
      }
   }
   return nil
}

// String returns the formatted help text for this specific set of flags.
func (fs FlagSet) String() string {
   data := &strings.Builder{}
   for i, f := range fs {
      if i > 0 {
         fmt.Fprintln(data)
      }
      fmt.Fprint(data, f)
   }
   return data.String()
}

// Parse loops through os.Args directly and checks against the FlagSet.
func (fs FlagSet) Parse() error {
   for i := 1; i < len(os.Args); i++ {
      arg := os.Args[i]

      if len(arg) < 2 || arg[0] != '-' {
         return fmt.Errorf("unexpected argument or invalid flag format: %s", arg)
      }

      parsedName := arg[1:]
      found := fs.Lookup(parsedName)

      if found == nil {
         return fmt.Errorf("flag provided but not defined: %s", arg)
      }

      found.Set = true

      if found.HasValue {
         if i+1 < len(os.Args) {
            found.Value = os.Args[i+1]
            i++ // consume the value so it isn't processed as a flag in the next iteration
         } else {
            return fmt.Errorf("flag '-%s' requires a value", found.Name)
         }
      }
   }
   return nil
}

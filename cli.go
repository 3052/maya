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

type Flag struct {
   Group    int
   Name     string
   HasValue bool
   Set      bool
   Value    string
}

func (f *Flag) String() string {
   var builder strings.Builder
   builder.WriteByte('\t')
   builder.WriteString(f.Name)
   if f.HasValue {
      builder.WriteString(" value")
   }
   return builder.String()
}

func (f *Flag) ParseInt() (int, error) {
   result, err := strconv.Atoi(f.Value)
   if err != nil {
      return 0, fmt.Errorf("invalid value %q for flag %q: %v", f.Value, f.Name, err)
   }
   return result, nil
}

func (f *Flag) ParseUrl() (*url.URL, error) {
   result, err := url.Parse(f.Value)
   if err != nil {
      return nil, fmt.Errorf("invalid value %q for flag %q: %v", f.Value, f.Name, err)
   }
   return result, nil
}

type FlagSet []*Flag

func (fs *FlagSet) Add(group int, name string) *Flag {
   f := &Flag{
      Group:    group,
      HasValue: false,
      Name:     name,
   }
   *fs = append(*fs, f)
   return f
}

func (fs *FlagSet) AddValue(group int, name string) *Flag {
   f := &Flag{
      Group:    group,
      HasValue: true,
      Name:     name,
   }
   *fs = append(*fs, f)
   return f
}

// Lookup returns the Flag that matches the given key within its Name string.
// It returns an error if zero flags match, or if multiple flags match.
func (fs FlagSet) Lookup(key string) (*Flag, error) {
   var matched *Flag
   var matchCount int

   for _, f := range fs {
      if strings.Contains(f.Name, key) {
         matched = f
         matchCount++
      }
   }

   if matchCount == 0 {
      return nil, fmt.Errorf("unknown flag: %s", key)
   }
   if matchCount > 1 {
      return nil, fmt.Errorf("ambiguous flag: %s matches multiple definitions", key)
   }

   return matched, nil
}

func (fs FlagSet) Parse() error {
   for i := 1; i < len(os.Args); i++ {
      key := os.Args[i]

      matched, err := fs.Lookup(key)
      if err != nil {
         return err
      }

      matched.Set = true

      if matched.HasValue {
         if i+1 >= len(os.Args) {
            return fmt.Errorf("flag '%s' requires a value", key)
         }
         i++
         matched.Value = os.Args[i]
      }
   }
   return nil
}

func (fs FlagSet) hasMultipleGroups() bool {
   for i := 1; i < len(fs); i++ {
      if fs[i].Group != fs[0].Group {
         return true
      }
   }
   return false
}

func (fs FlagSet) String() string {
   var builder strings.Builder
   builder.WriteString("Usage: provide any unique substring to match a flag")

   multi := fs.hasMultipleGroups()
   var currentGroup int
   first := true

   for _, f := range fs {
      if multi && (first || f.Group != currentGroup) {
         if !first {
            builder.WriteByte('\n')
         }
         fmt.Fprintf(&builder, "\nGroup %d:", f.Group)
         currentGroup = f.Group
      }
      builder.WriteByte('\n')
      fmt.Fprint(&builder, f)
      first = false
   }

   return builder.String()
}

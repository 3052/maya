// cli.go
package maya

import (
   "encoding/xml"
   "fmt"
   "io"
   "log"
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

type FlagValue interface {
   Parse(string) error
   Type() string
   Default() string
   Example() string
}

type FlagString string

func (s *FlagString) Parse(value string) error {
   *s = FlagString(value)
   return nil
}

func (FlagString) Type() string {
   return "string"
}

func (s FlagString) Default() string {
   return string(s)
}

func (FlagString) Example() string {
   return "xyz"
}

type FlagInt int

func (i *FlagInt) Parse(value string) error {
   parsed, err := strconv.Atoi(value)
   if err != nil {
      return err
   }
   *i = FlagInt(parsed)
   return nil
}

func (FlagInt) Type() string {
   return "int"
}

func (i FlagInt) Default() string {
   if i != 0 {
      return strconv.Itoa(int(i))
   }
   return ""
}

func (FlagInt) Example() string {
   return "789"
}

type FlagBool bool

func (b *FlagBool) Parse(value string) error {
   if value == "" {
      *b = true
      return nil
   }
   parsed, err := strconv.ParseBool(value)
   if err != nil {
      return err
   }
   *b = FlagBool(parsed)
   return nil
}

func (FlagBool) Type() string {
   return "bool"
}

func (b FlagBool) Default() string {
   if b {
      return "true"
   }
   return ""
}

func (FlagBool) Example() string {
   return ""
}

type Flag struct {
   Name  string
   Usage string
   Value FlagValue
   Needs string
   isSet bool
}

type FlagSet []*Flag

func (set FlagSet) lookup(name string) *Flag {
   for _, item := range set {
      if item.Name == name {
         return item
      }
   }
   return nil
}

func (set FlagSet) IsSet(value FlagValue) bool {
   for _, item := range set {
      if item.Value == value {
         return item.isSet
      }
   }
   return false
}

func (set FlagSet) Parse(args []string) error {
   for _, arg := range args {
      name, value, _ := strings.Cut(arg, "=")

      if name == "" {
         return fmt.Errorf("bad flag syntax: %s", arg)
      }

      var matched *Flag
      var matchCount int

      for _, item := range set {
         if strings.HasPrefix(item.Name, name) {
            matched = item
            matchCount++
         }
      }

      if matchCount == 0 {
         return fmt.Errorf("flag provided but not defined: %s", name)
      }
      if matchCount > 1 {
         return fmt.Errorf("ambiguous flag: %s", name)
      }

      if err := matched.Value.Parse(value); err != nil {
         return fmt.Errorf("invalid value %q for flag %s: %v", value, matched.Name, err)
      }

      matched.isSet = true
   }

   return nil
}

func (set FlagSet) Usage(w io.Writer, name string) error {
   data := new(strings.Builder)

   // --- 1. Flags Section ---
   for _, item := range set {
      nameAndType := item.Name + " " + item.Value.Type()
      def := item.Value.Default()

      fmt.Fprintf(data, "%s\n", nameAndType)

      if item.Usage != "" {
         fmt.Fprintf(data, "\tusage: %s\n", item.Usage)
      }
      if def != "" {
         fmt.Fprintf(data, "\tdefault: %s\n", def)
      }
   }

   // --- 2. Examples Section ---
   fmt.Fprint(data, "\nexamples:\n")

   formatFlag := func(f *Flag) string {
      firstLetter := f.Name[:1]
      count := 0
      for _, x := range set {
         if strings.HasPrefix(x.Name, firstLetter) {
            count++
         }
      }

      prefix := f.Name
      if count == 1 {
         prefix = firstLetter
      }

      if ex := f.Value.Example(); ex != "" {
         return prefix + "=" + ex
      }
      return prefix
   }

   for _, item := range set {
      fmt.Fprintf(data, "\t%s", name)
      if item.Needs != "" {
         needed := set.lookup(item.Needs)
         if needed == nil {
            return fmt.Errorf("flag %q needs undefined flag %q", item.Name, item.Needs)
         }
         fmt.Fprintf(data, " %s", formatFlag(needed))
      }
      fmt.Fprintf(data, " %s\n", formatFlag(item))
   }

   _, err := fmt.Fprint(w, data)
   return err
}

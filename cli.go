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

// Cache holds the pre-computed OS path for the cache directory.
type Cache string

// Decode reads the XML from the cache directory and populates the structs.
// It stops and returns an error on the first failure.
func (c Cache) Decode(values ...any) error {
   for _, value := range values {
      filename := c.GetFilePath(value)
      data, err := os.ReadFile(filename)
      if err != nil {
         return err
      }
      err = xml.Unmarshal(data, value)
      if err != nil {
         return fmt.Errorf("failed to decode XML for %T: %w", value, err)
      }
   }
   return nil
}

// Encode marshals the values and writes them to the cache directory.
// It stops and returns an error on the first failure.
func (c Cache) Encode(values ...any) error {
   for _, value := range values {
      filename := c.GetFilePath(value)

      data, err := xml.MarshalIndent(value, "", "  ")
      if err != nil {
         // Added type info to the error to know WHICH item failed
         return fmt.Errorf("failed to encode XML for %T: %w", value, err)
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
func (c Cache) GetFilePath(value any) string {
   valueType := reflect.TypeOf(value)
   for valueType.Kind() == reflect.Ptr {
      valueType = valueType.Elem()
   }

   return filepath.Join(string(c), valueType.Name()+".xml")
}

// Setup computes the full cache path, creates the directory exactly once,
// and stores the path in the Cache type.
func (c *Cache) Setup(dirName string) error {
   cacheDir, err := os.UserCacheDir()
   if err != nil {
      return fmt.Errorf("failed to get cache directory: %w", err)
   }

   // Update the underlying string value (requires pointer receiver)
   *c = Cache(filepath.Join(cacheDir, dirName))

   // Create the directory immediately upon setup
   if err := os.MkdirAll(string(*c), os.ModePerm); err != nil {
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
         // Validate inside the existing loop
         if item.Name == "" {
            return fmt.Errorf("flag name cannot be empty")
         }

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
         return fmt.Errorf("invalid value for flag %s: %v", matched.Name, err)
      }

      matched.isSet = true
   }

   return nil
}

func (set FlagSet) Usage(w io.Writer, name string) error {
   data := new(strings.Builder)

   // --- 1. Index Section ---
   fmt.Fprint(data, "index:\n")
   for _, item := range set {
      // Validate inside the existing loop (protects the next section from panicking)
      if item.Name == "" {
         return fmt.Errorf("flag name cannot be empty")
      }

      nameAndType := item.Name + " " + item.Value.Type()
      def := item.Value.Default()

      fmt.Fprintf(data, "     %s\n", nameAndType)

      if item.Usage != "" {
         fmt.Fprintf(data, "          usage: %s\n", item.Usage)
      }
      if def != "" {
         fmt.Fprintf(data, "          default: %s\n", def)
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
      fmt.Fprintf(data, "     %s", name)
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

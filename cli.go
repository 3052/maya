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

// Flag represents a command-line flag with no value.
type Flag struct {
   Set bool // Determines if the flag was used
}

// StringFlag represents a command-line flag with a string value.
type StringFlag struct {
   Set   bool
   Value string
}

// IntFlag represents a command-line flag with an integer value.
type IntFlag struct {
   Set   bool
   Value int
}

// UrlFlag represents a command-line flag with a parsed URL value.
type UrlFlag struct {
   Set   bool
   Value *url.URL
}

var (
   typeFlag       = reflect.TypeOf(Flag{})
   typeStringFlag = reflect.TypeOf(StringFlag{})
   typeIntFlag    = reflect.TypeOf(IntFlag{})
   typeUrlFlag    = reflect.TypeOf(UrlFlag{})
)

// isFlagType checks if the given type is one of our supported flag structs.
func isFlagType(t reflect.Type) bool {
   return t == typeFlag || t == typeStringFlag || t == typeIntFlag || t == typeUrlFlag
}

// ParseFlags reads os.Args and populates the provided struct pointer.
// It automatically binds to any fields of the supported flag types, allowing partial string matches.
func ParseFlags(target any) error {
   rv := reflect.ValueOf(target)

   // Ensure we were passed a pointer to a struct
   if rv.Kind() != reflect.Ptr || rv.Elem().Kind() != reflect.Struct {
      return fmt.Errorf("expected a pointer to a struct")
   }

   elem := rv.Elem()
   targetType := elem.Type()

   // Parse os.Args
   for i := 1; i < len(os.Args); i++ {
      argName := os.Args[i]

      var matchedIndex int
      var matches []string

      // Scan the struct fields for matching flag names
      for j := 0; j < targetType.NumField(); j++ {
         field := targetType.Field(j)

         if !isFlagType(field.Type) {
            continue
         }

         if strings.Contains(field.Name, argName) {
            matches = append(matches, field.Name)
            matchedIndex = j
         }
      }

      if len(matches) == 0 {
         return fmt.Errorf("unknown flag: %s", argName)
      }
      if len(matches) > 1 {
         return fmt.Errorf("ambiguous flag: '%s' matches multiple fields %v", argName, matches)
      }

      matchedField := targetType.Field(matchedIndex)
      fieldVal := elem.Field(matchedIndex)

      // Mark the flag as used
      fieldVal.FieldByName("Set").SetBool(true)

      // If it's not the base Flag (boolean), it needs a value parsed
      if matchedField.Type != typeFlag {
         if i+1 < len(os.Args) {
            valStr := os.Args[i+1]

            switch matchedField.Type {
            case typeStringFlag:
               fieldVal.FieldByName("Value").SetString(valStr)
            case typeIntFlag:
               number, err := strconv.Atoi(valStr)
               if err != nil {
                  return fmt.Errorf("invalid value %q for flag %s: %v", valStr, matchedField.Name, err)
               }
               fieldVal.FieldByName("Value").SetInt(int64(number))
            case typeUrlFlag:
               address, err := url.Parse(valStr)
               if err != nil {
                  return fmt.Errorf("invalid value %q for flag %s: %v", valStr, matchedField.Name, err)
               }
               fieldVal.FieldByName("Value").Set(reflect.ValueOf(address))
            }

            i++ // skip the value argument in the main loop
         } else {
            return fmt.Errorf("flag %s requires a value", matchedField.Name)
         }
      }
   }

   return nil
}

// FormatFlags inspects the provided struct pointer and prints an auto-generated
// help menu to stdout, relying on the struct's field order for grouping.
func FormatFlags(target any) error {
   rv := reflect.ValueOf(target)

   if rv.Kind() != reflect.Ptr || rv.Elem().Kind() != reflect.Struct {
      return fmt.Errorf("expected a pointer to a struct")
   }

   targetType := rv.Elem().Type()

   fmt.Printf("Usage: flags can be matched by any unique substring\n")

   var currentGroup string

   for i := 0; i < targetType.NumField(); i++ {
      field := targetType.Field(i)

      if !isFlagType(field.Type) {
         continue
      }

      group := field.Tag.Get("group")

      // If the group changed and isn't empty, print the new group header
      if group != "" && group != currentGroup {
         currentGroup = group
         fmt.Printf("\n%s:\n", group)
      }

      // Determine formatting based on whether it needs a value
      if field.Type == typeFlag {
         fmt.Printf("\t%s\n", field.Name)
      } else {
         fmt.Printf("\t%s value\n", field.Name)
      }
   }

   return nil
}

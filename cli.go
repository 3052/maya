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
   Set bool
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

func isFlagType(t reflect.Type) bool {
   return t == typeFlag || t == typeStringFlag || t == typeIntFlag || t == typeUrlFlag
}

// ParseFlags reads os.Args and populates the provided struct pointer.
// It automatically binds to flags directly or inside nested group structs.
func ParseFlags(target any) error {
   rv := reflect.ValueOf(target)

   if rv.Kind() != reflect.Ptr || rv.Elem().Kind() != reflect.Struct {
      return fmt.Errorf("expected a pointer to a struct")
   }

   elem := rv.Elem()
   targetType := elem.Type()

   for i := 1; i < len(os.Args); i++ {
      argName := os.Args[i]

      var matches []string
      var matchedVal reflect.Value
      var matchedType reflect.Type
      var matchedName string

      // Scan top-level fields and nested struct fields
      for j := 0; j < targetType.NumField(); j++ {
         field := targetType.Field(j)
         fieldVal := elem.Field(j)

         if isFlagType(field.Type) {
            if strings.Contains(field.Name, argName) {
               matches = append(matches, field.Name)
               matchedVal = fieldVal
               matchedType = field.Type
               matchedName = field.Name
            }
         } else if field.Type.Kind() == reflect.Struct {
            // Search inside the nested group struct
            for k := 0; k < field.Type.NumField(); k++ {
               subField := field.Type.Field(k)
               subFieldVal := fieldVal.Field(k)

               if isFlagType(subField.Type) {
                  if strings.Contains(subField.Name, argName) {
                     matches = append(matches, subField.Name)
                     matchedVal = subFieldVal
                     matchedType = subField.Type
                     matchedName = subField.Name
                  }
               }
            }
         }
      }

      if len(matches) == 0 {
         return fmt.Errorf("unknown flag: %s", argName)
      }
      if len(matches) > 1 {
         return fmt.Errorf("ambiguous flag: '%s' matches multiple fields %v", argName, matches)
      }

      // Mark the flag as used
      matchedVal.FieldByName("Set").SetBool(true)

      // Parse the value if it's not a boolean flag
      if matchedType != typeFlag {
         if i+1 < len(os.Args) {
            valStr := os.Args[i+1]

            switch matchedType {
            case typeStringFlag:
               matchedVal.FieldByName("Value").SetString(valStr)
            case typeIntFlag:
               number, err := strconv.Atoi(valStr)
               if err != nil {
                  return fmt.Errorf("invalid value %q for flag %s: %v", valStr, matchedName, err)
               }
               matchedVal.FieldByName("Value").SetInt(int64(number))
            case typeUrlFlag:
               address, err := url.Parse(valStr)
               if err != nil {
                  return fmt.Errorf("invalid value %q for flag %s: %v", valStr, matchedName, err)
               }
               matchedVal.FieldByName("Value").Set(reflect.ValueOf(address))
            }

            i++ // skip the value argument in the main loop
         } else {
            return fmt.Errorf("flag %s requires a value", matchedName)
         }
      }
   }

   return nil
}

// FormatFlags inspects the provided struct pointer and prints an auto-generated
// help menu to stdout, relying on the struct's layout for grouping.
func FormatFlags(target any) error {
   rv := reflect.ValueOf(target)

   if rv.Kind() != reflect.Ptr || rv.Elem().Kind() != reflect.Struct {
      return fmt.Errorf("expected a pointer to a struct")
   }

   targetType := rv.Elem().Type()

   fmt.Printf("Usage: flags can be matched by any unique substring\n")

   for i := 0; i < targetType.NumField(); i++ {
      field := targetType.Field(i)

      if isFlagType(field.Type) {
         if field.Type == typeFlag {
            fmt.Printf("\t%s\n", field.Name)
         } else {
            fmt.Printf("\t%s value\n", field.Name)
         }
      } else if field.Type.Kind() == reflect.Struct {
         // Print the nested struct's name as the group header
         fmt.Printf("\n%s:\n", field.Name)

         for j := 0; j < field.Type.NumField(); j++ {
            subField := field.Type.Field(j)
            if isFlagType(subField.Type) {
               if subField.Type == typeFlag {
                  fmt.Printf("\t%s\n", subField.Name)
               } else {
                  fmt.Printf("\t%s value\n", subField.Name)
               }
            }
         }
      }
   }

   return nil
}

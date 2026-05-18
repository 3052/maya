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

// Flag represents a command-line flag. T determines the expected value type.
type Flag[T bool | string | int] struct {
   Set   bool
   Value T
}

// isFlagType checks if the given type is a Flag[T] by inspecting its structure.
func isFlagType(t reflect.Type) bool {
   return t.Kind() == reflect.Struct &&
      t.NumField() == 2 &&
      t.Field(0).Name == "Set" &&
      t.Field(1).Name == "Value"
}

// ParseFlags reads the provided args and populates the struct pointer.
func ParseFlags(args []string, target any) error {
   rv := reflect.ValueOf(target)

   if rv.Kind() != reflect.Ptr || rv.Elem().Kind() != reflect.Struct {
      return fmt.Errorf("expected a pointer to a struct")
   }

   elem := rv.Elem()
   targetType := elem.Type()

   for i := 0; i < len(args); i++ {
      argName := args[i]

      var matchedIndex int
      var matches []string

      // Scan the struct fields for matching flag names
      for j := 0; j < targetType.NumField(); j++ {
         field := targetType.Field(j)

         if !isFlagType(field.Type) {
            continue
         }

         if strings.HasPrefix(field.Name, argName) {
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

      valueField := fieldVal.FieldByName("Value")
      valueType := valueField.Type()

      // If it's a bool flag, it doesn't need an accompanying CLI value
      if valueType.Kind() == reflect.Bool {
         valueField.SetBool(true)
      } else {
         if i+1 < len(args) {
            valStr := args[i+1]

            if valueType.Kind() == reflect.String {
               valueField.SetString(valStr)
            } else if valueType.Kind() == reflect.Int {
               number, err := strconv.Atoi(valStr)
               if err != nil {
                  return fmt.Errorf("invalid flag %s: %v", matchedField.Name, err)
               }
               valueField.SetInt(int64(number))
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
// human-readable help menu to the provided io.Writer.
func FormatFlags(w io.Writer, cmdName string, target any) error {
   rv := reflect.ValueOf(target)

   if rv.Kind() != reflect.Ptr || rv.Elem().Kind() != reflect.Struct {
      return fmt.Errorf("expected a pointer to a struct")
   }

   targetType := rv.Elem().Type()

   fmt.Fprintf(w, "Overview: flags can be matched by any unique prefix\n\n")
   fmt.Fprintln(w, "Index:")

   // Pass 0: Count occurrences of the first letter (byte) of every flag
   firstLetterCount := make(map[byte]int)
   for i := 0; i < targetType.NumField(); i++ {
      field := targetType.Field(i)
      if isFlagType(field.Type) && len(field.Name) > 0 {
         firstLetterCount[field.Name[0]]++
      }
   }

   // Store a string representation of how each flag looks when used.
   dummies := make(map[string]string)

   // Pass 1: Print the Index and build the dummy value strings
   for i := 0; i < targetType.NumField(); i++ {
      field := targetType.Field(i)

      if !isFlagType(field.Type) || len(field.Name) == 0 {
         continue
      }

      valueType := field.Type.Field(1).Type
      usage := field.Tag.Get("usage")

      // Decide if we should use the single letter or full word for the example
      firstLetter := field.Name[0]
      exampleFlagName := field.Name
      if firstLetterCount[firstLetter] == 1 {
         exampleFlagName = string(firstLetter)
      }

      // Print the flag name using a tab
      if valueType.Kind() == reflect.Bool {
         fmt.Fprintf(w, "\t%s\n", field.Name)
         dummies[field.Name] = exampleFlagName
      } else {
         fmt.Fprintf(w, "\t%s %s\n", field.Name, valueType)

         // Assign dummy values based on type
         if valueType.Kind() == reflect.String {
            dummies[field.Name] = exampleFlagName + " xyz"
         } else if valueType.Kind() == reflect.Int {
            dummies[field.Name] = exampleFlagName + " 123"
         }
      }

      // Print usage on the next line indented with two tabs to offset from the flag
      if usage != "" {
         fmt.Fprintf(w, "\t\t%s\n", usage)
      }
   }

   fmt.Fprintf(w, "\nExamples:\n")

   // Pass 2: Generate the examples based on the 'depends' tag
   for i := 0; i < targetType.NumField(); i++ {
      field := targetType.Field(i)

      if !isFlagType(field.Type) {
         continue
      }

      myDummy := dummies[field.Name]
      depends := field.Tag.Get("depends")

      // If it has a dependency, print command + parent dummy + my dummy
      if depends != "" && dummies[depends] != "" {
         fmt.Fprintf(w, "\t%s %s %s\n", cmdName, dummies[depends], myDummy)
      } else {
         // Otherwise print command + my dummy
         fmt.Fprintf(w, "\t%s %s\n", cmdName, myDummy)
      }
   }

   return nil
}

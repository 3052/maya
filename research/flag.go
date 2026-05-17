package myflag

import (
   "fmt"
   "net/url"
   "os"
   "reflect"
   "strconv"
   "strings"
)

// FlagSpace is a zero-byte struct used to create visual breaks in the FormatFlags output.
type FlagSpace struct{}

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
   typeFlagSpace  = reflect.TypeOf(FlagSpace{})
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
// human-readable help menu to stdout. Examples are optional.
func FormatFlags(target any, examples ...string) error {
   rv := reflect.ValueOf(target)

   if rv.Kind() != reflect.Ptr || rv.Elem().Kind() != reflect.Struct {
      return fmt.Errorf("expected a pointer to a struct")
   }

   targetType := rv.Elem().Type()

   fmt.Printf("Overview: flags can be matched by any unique substring\n\n")
   fmt.Println("Index:")

   for i := 0; i < targetType.NumField(); i++ {
      field := targetType.Field(i)

      if field.Type == typeFlagSpace {
         fmt.Println()
         continue
      }

      if !isFlagType(field.Type) {
         continue
      }

      // Determine formatting based on whether it needs a value
      if field.Type == typeFlag {
         fmt.Printf("\t%s\n", field.Name)
      } else {
         fmt.Printf("\t%s value\n", field.Name)
      }
   }

   // Only print the Examples section if the user provided examples
   if len(examples) > 0 {
      fmt.Printf("\nExamples:\n")
      for _, example := range examples {
         fmt.Printf("\t%s\n", example)
      }
   }

   return nil
}

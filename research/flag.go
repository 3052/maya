package myflag

import (
   "fmt"
   "reflect"
   "strconv"
   "strings"
)

// FlagSpace is a zero-byte struct used to create visual breaks in the FormatFlags output.
type FlagSpace struct{}

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
// It automatically binds to any fields of type Flag[T], allowing partial string matches.
func ParseFlags(target any, args []string) error {
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
                  return fmt.Errorf("invalid value %q for flag %s: %v", valStr, matchedField.Name, err)
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
// human-readable help menu to stdout. Examples are optional.
func FormatFlags(target any, examples ...string) error {
   rv := reflect.ValueOf(target)

   if rv.Kind() != reflect.Ptr || rv.Elem().Kind() != reflect.Struct {
      return fmt.Errorf("expected a pointer to a struct")
   }

   targetType := rv.Elem().Type()
   typeFlagSpace := reflect.TypeOf(FlagSpace{})

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

      valueType := field.Type.Field(1).Type

      if valueType.Kind() == reflect.Bool {
         fmt.Printf("\t%s\n", field.Name)
      } else {
         fmt.Printf("\t%s %s\n", field.Name, valueType)
      }
   }

   if len(examples) > 0 {
      fmt.Printf("\nExamples:\n")
      for _, example := range examples {
         fmt.Printf("\t%s\n", example)
      }
   }

   return nil
}

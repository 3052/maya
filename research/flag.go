package maya

import (
   "fmt"
   "io"
   "reflect"
   "strconv"
   "strings"
)

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

   // Pass 0: Count occurrences of the first letter of every flag
   firstLetterCount := make(map[string]int)
   for i := 0; i < targetType.NumField(); i++ {
      field := targetType.Field(i)
      if isFlagType(field.Type) && len(field.Name) > 0 {
         firstLetterCount[string(field.Name[0])]++
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

      // Decide if we should use the single letter or full word for the example
      firstLetter := string(field.Name[0])
      exampleFlagName := field.Name
      if firstLetterCount[firstLetter] == 1 {
         exampleFlagName = firstLetter
      }

      if valueType.Kind() == reflect.Bool {
         fmt.Fprintf(w, "\t%s\n", field.Name)
         dummies[field.Name] = exampleFlagName // Bools don't need values
      } else {
         fmt.Fprintf(w, "\t%s %s\n", field.Name, valueType)

         // Assign dummy values based on type
         if valueType.Kind() == reflect.String {
            dummies[field.Name] = exampleFlagName + " xyz"
         } else if valueType.Kind() == reflect.Int {
            dummies[field.Name] = exampleFlagName + " 123"
         }
      }
   }

   fmt.Fprintf(w, "\nExamples:\n")

   // Format the command prefix safely
   prefix := ""
   if cmdName != "" {
      prefix = cmdName + " "
   }

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
         fmt.Fprintf(w, "\t%s%s %s\n", prefix, dummies[depends], myDummy)
      } else {
         // Otherwise print command + my dummy
         fmt.Fprintf(w, "\t%s%s\n", prefix, myDummy)
      }
   }

   return nil
}

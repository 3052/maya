package myflag

import (
   "fmt"
   "os"
   "reflect"
   "strings"
)

// Flag represents a single command-line flag.
type Flag struct {
   Name     string
   HasValue bool   // Determines if the flag expects an accompanying value
   IsSet    bool   // Determines if the flag was used
   Value    string // The raw value passed after the flag (if HasValue is true)
}

// Parse reads os.Args and populates the provided struct pointer.
// Struct fields must be of type Flag or *Flag.
func Parse(v any) error {
   rv := reflect.ValueOf(v)

   // Ensure we were passed a pointer to a struct
   if rv.Kind() != reflect.Ptr || rv.Elem().Kind() != reflect.Struct {
      return fmt.Errorf("expected a pointer to a struct")
   }

   elem := rv.Elem()
   t := elem.Type()

   type structField struct {
      index int
      isPtr bool
   }

   registeredFlags := make(map[string]structField)

   flagType := reflect.TypeOf(Flag{})
   flagPtrType := reflect.TypeOf(&Flag{})

   // 1. Inspect the struct and build our map of allowed flags from the tags
   for i := 0; i < t.NumField(); i++ {
      field := t.Field(i)
      tag := field.Tag.Get("flag")
      if tag == "" {
         continue // Skip fields without the flag tag
      }

      // Parse the tag to get the name and see if it requires a value
      // e.g., `flag:"port,hasvalue"`
      parts := strings.Split(tag, ",")
      name := parts[0]
      hasValue := false
      if len(parts) > 1 && parts[1] == "hasvalue" {
         hasValue = true
      }

      isPtr := false
      if field.Type == flagPtrType {
         isPtr = true
      } else if field.Type != flagType {
         return fmt.Errorf("field %s for flag %s must be Flag or *Flag", field.Name, name)
      }

      fieldVal := elem.Field(i)

      // Initialize the pointer if it is currently nil
      if isPtr && fieldVal.IsNil() {
         fieldVal.Set(reflect.ValueOf(&Flag{}))
      }

      // Auto-populate the Name and HasValue fields directly from the tag
      var flagElem reflect.Value
      if isPtr {
         flagElem = fieldVal.Elem()
      } else {
         flagElem = fieldVal
      }

      flagElem.FieldByName("Name").SetString(name)
      flagElem.FieldByName("HasValue").SetBool(hasValue)

      registeredFlags[name] = structField{
         index: i,
         isPtr: isPtr,
      }
   }

   // 2. Parse os.Args
   for i := 1; i < len(os.Args); i++ {
      name := os.Args[i]

      fInfo, exists := registeredFlags[name]
      if !exists {
         return fmt.Errorf("unknown flag: %s", name)
      }

      fieldVal := elem.Field(fInfo.index)
      var flagElem reflect.Value
      if fInfo.isPtr {
         flagElem = fieldVal.Elem()
      } else {
         flagElem = fieldVal
      }

      // Mark the flag as used
      flagElem.FieldByName("IsSet").SetBool(true)

      // If this flag requires a value, grab the next argument
      if flagElem.FieldByName("HasValue").Bool() {
         if i+1 < len(os.Args) {
            i++ // move to the next arg
            flagElem.FieldByName("Value").SetString(os.Args[i])
         } else {
            return fmt.Errorf("flag %s requires a value", name)
         }
      }
   }

   return nil
}

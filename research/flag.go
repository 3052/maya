package myflag

import (
   "fmt"
   "os"
   "reflect"
)

// Flag represents a single command-line flag.
type Flag struct {
   Name     string
   HasValue bool   // Determines if the flag expects an accompanying value
   Set      bool   // Determines if the flag was used
   Value    string // The raw value passed after the flag (if HasValue is true)
   Group    string // The group this flag belongs to
}

// Parse reads os.Args and populates the provided struct pointer.
// It automatically binds to any fields of type Flag using the exact field name.
func Parse(target any) error {
   rv := reflect.ValueOf(target)

   // Ensure we were passed a pointer to a struct
   if rv.Kind() != reflect.Ptr || rv.Elem().Kind() != reflect.Struct {
      return fmt.Errorf("expected a pointer to a struct")
   }

   elem := rv.Elem()
   t := elem.Type()

   // Map flag name to its field index in the struct
   registeredFlags := make(map[string]int)
   flagType := reflect.TypeOf(Flag{})

   // 1. Inspect the struct and build our map of flags based on field names
   for i := 0; i < t.NumField(); i++ {
      field := t.Field(i)

      // Skip any fields that aren't exactly of type Flag
      if field.Type != flagType {
         continue
      }

      name := field.Name
      hasValue := field.Tag.Get("flag") == "value"
      group := field.Tag.Get("group")

      // Auto-populate the Name, HasValue, and Group fields
      fieldVal := elem.Field(i)
      fieldVal.FieldByName("Name").SetString(name)
      fieldVal.FieldByName("HasValue").SetBool(hasValue)
      fieldVal.FieldByName("Group").SetString(group)

      registeredFlags[name] = i
   }

   // 2. Parse os.Args
   for i := 1; i < len(os.Args); i++ {
      name := os.Args[i]

      fieldIndex, exists := registeredFlags[name]
      if !exists {
         return fmt.Errorf("unknown flag: %s", name)
      }

      fieldVal := elem.Field(fieldIndex)

      // Mark the flag as used
      fieldVal.FieldByName("Set").SetBool(true)

      // If this flag requires a value, grab the next argument
      if fieldVal.FieldByName("HasValue").Bool() {
         if i+1 < len(os.Args) {
            i++ // move to the next arg
            fieldVal.FieldByName("Value").SetString(os.Args[i])
         } else {
            return fmt.Errorf("flag %s requires a value", name)
         }
      }
   }

   return nil
}

// Usage inspects the provided struct pointer and prints an auto-generated
// help menu to stderr, organized by group.
func Usage(target any) {
   rv := reflect.ValueOf(target)

   if rv.Kind() != reflect.Ptr || rv.Elem().Kind() != reflect.Struct {
      return
   }

   elem := rv.Elem()
   t := elem.Type()
   flagType := reflect.TypeOf(Flag{})

   progName := os.Args[0]
   fmt.Fprintf(os.Stderr, "Usage of %s:\n", progName)

   type displayFlag struct {
      name     string
      hasValue bool
   }

   var ungrouped []displayFlag
   grouped := make(map[string][]displayFlag)
   var groupOrder []string

   for i := 0; i < t.NumField(); i++ {
      field := t.Field(i)

      if field.Type != flagType {
         continue
      }

      group := field.Tag.Get("group")
      df := displayFlag{
         name:     field.Name,
         hasValue: field.Tag.Get("flag") == "value",
      }

      if group == "" {
         ungrouped = append(ungrouped, df)
      } else {
         if _, exists := grouped[group]; !exists {
            groupOrder = append(groupOrder, group) // Keep insertion order for headers
         }
         grouped[group] = append(grouped[group], df)
      }
   }

   // Print ungrouped flags first
   for _, f := range ungrouped {
      if f.hasValue {
         fmt.Fprintf(os.Stderr, "  %s value\n", f.name)
      } else {
         fmt.Fprintf(os.Stderr, "  %s\n", f.name)
      }
   }

   // Print grouped flags with headers
   for _, g := range groupOrder {
      fmt.Fprintf(os.Stderr, "\n%s:\n", g)
      for _, f := range grouped[g] {
         if f.hasValue {
            fmt.Fprintf(os.Stderr, "  %s value\n", f.name)
         } else {
            fmt.Fprintf(os.Stderr, "  %s\n", f.name)
         }
      }
   }
}

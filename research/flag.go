package myflag

import (
   "fmt"
   "os"
   "reflect"
   "strings"
)

// Flag represents a single command-line flag.
type Flag struct {
   Name  string
   Set   bool   // Determines if the flag was used
   Value string // The raw value passed after the flag (if value:"true" tag was used)
   Group string // The group this flag belongs to
}

// Parse reads os.Args and populates the provided struct pointer.
// It automatically binds to any fields of type Flag, allowing partial string matches.
func Parse(target any) error {
   rv := reflect.ValueOf(target)

   // Ensure we were passed a pointer to a struct
   if rv.Kind() != reflect.Ptr || rv.Elem().Kind() != reflect.Struct {
      return fmt.Errorf("expected a pointer to a struct")
   }

   elem := rv.Elem()
   t := elem.Type()

   type flagInfo struct {
      index    int
      hasValue bool
   }

   // Map flag name to its index and value requirement
   registeredFlags := make(map[string]flagInfo)
   flagType := reflect.TypeOf(Flag{})

   // 1. Inspect the struct and build our map of flags based on field names
   for i := 0; i < t.NumField(); i++ {
      field := t.Field(i)

      // Skip any fields that aren't exactly of type Flag
      if field.Type != flagType {
         continue
      }

      name := field.Name
      hasValue := field.Tag.Get("value") == "true"
      group := field.Tag.Get("group")

      // Auto-populate the Name and Group fields
      fieldVal := elem.Field(i)
      fieldVal.FieldByName("Name").SetString(name)
      fieldVal.FieldByName("Group").SetString(group)

      registeredFlags[name] = flagInfo{
         index:    i,
         hasValue: hasValue,
      }
   }

   // 2. Parse os.Args
   for i := 1; i < len(os.Args); i++ {
      argName := os.Args[i]
      var matchedName string

      // First, check for an exact match
      if _, exists := registeredFlags[argName]; exists {
         matchedName = argName
      } else {
         // If no exact match, look for partial matches using strings.Contains
         var matches []string
         for regName := range registeredFlags {
            if strings.Contains(regName, argName) {
               matches = append(matches, regName)
            }
         }

         if len(matches) == 0 {
            return fmt.Errorf("unknown flag: %s", argName)
         }
         if len(matches) > 1 {
            return fmt.Errorf("ambiguous flag: '%s' matches multiple fields %v", argName, matches)
         }
         matchedName = matches[0]
      }

      fInfo := registeredFlags[matchedName]
      fieldVal := elem.Field(fInfo.index)

      // Mark the flag as used
      fieldVal.FieldByName("Set").SetBool(true)

      // If this flag requires a value, grab the next argument
      if fInfo.hasValue {
         if i+1 < len(os.Args) {
            i++ // move to the next arg
            fieldVal.FieldByName("Value").SetString(os.Args[i])
         } else {
            return fmt.Errorf("flag %s requires a value", matchedName)
         }
      }
   }

   return nil
}

// Usage inspects the provided struct pointer and prints an auto-generated
// help menu to stdout, organized by group.
func Usage(target any) error {
   rv := reflect.ValueOf(target)

   if rv.Kind() != reflect.Ptr || rv.Elem().Kind() != reflect.Struct {
      return fmt.Errorf("expected a pointer to a struct")
   }

   elem := rv.Elem()
   t := elem.Type()
   flagType := reflect.TypeOf(Flag{})

   fmt.Printf("Usage: flags can be matched by any unique substring\n")

   type displayFlag struct {
      name     string
      hasValue bool
   }

   var ungrouped []displayFlag
   grouped := make(map[string][]displayFlag)
   var groupOrder []string // Preserves the order groups are discovered in the struct

   for i := 0; i < t.NumField(); i++ {
      field := t.Field(i)

      if field.Type != flagType {
         continue
      }

      group := field.Tag.Get("group")
      hasValue := field.Tag.Get("value") == "true"

      df := displayFlag{
         name:     field.Name,
         hasValue: hasValue,
      }

      if group == "" {
         ungrouped = append(ungrouped, df)
      } else {
         if _, exists := grouped[group]; !exists {
            groupOrder = append(groupOrder, group)
         }
         grouped[group] = append(grouped[group], df)
      }
   }

   // Print ungrouped flags first
   for _, f := range ungrouped {
      if f.hasValue {
         fmt.Printf("\t%s value\n", f.name)
      } else {
         fmt.Printf("\t%s\n", f.name)
      }
   }

   // Print grouped flags with headers (in the order they appeared in the struct)
   for _, group := range groupOrder {
      fmt.Printf("\n%s:\n", group)
      for _, f := range grouped[group] {
         if f.hasValue {
            fmt.Printf("\t%s value\n", f.name)
         } else {
            fmt.Printf("\t%s\n", f.name)
         }
      }
   }

   return nil
}

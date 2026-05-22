package myflag

import (
   "fmt"
   "io"
   "reflect"
   "strconv"
   "strings"
)

type Flag[T bool | string | int] struct {
   Usage    string
   Value    T
   Set      bool
   Requires []string
}

var flagTypes = map[reflect.Type]bool{
   reflect.TypeOf(Flag[bool]{}):   true,
   reflect.TypeOf(Flag[string]{}): true,
   reflect.TypeOf(Flag[int]{}):    true,
}

func ParseFlags(arguments []string, target any) error {
   value := reflect.ValueOf(target)
   if value.Kind() != reflect.Ptr || value.Elem().Kind() != reflect.Struct {
      return fmt.Errorf("target must be a pointer to a struct")
   }
   value = value.Elem()
   targetType := value.Type()

   for i := 0; i < len(arguments); i++ {
      name := arguments[i]

      var matchCount, matchIndex int

      for j := 0; j < targetType.NumField(); j++ {
         structField := targetType.Field(j)

         if !flagTypes[structField.Type] {
            continue
         }

         if structField.Name == name {
            matchIndex = j
            matchCount = 1
            break
         }

         if strings.HasPrefix(structField.Name, name) {
            matchIndex = j
            matchCount++
         }
      }

      if matchCount == 0 {
         return fmt.Errorf("flag provided but not defined: %s", name)
      }
      if matchCount > 1 {
         return fmt.Errorf("flag %q is ambiguous", name)
      }

      fieldVal := value.Field(matchIndex)
      fieldVal.FieldByName("Set").SetBool(true)
      valueField := fieldVal.FieldByName("Value")

      switch valueField.Kind() {
      case reflect.Bool:
         valueField.SetBool(true)
      case reflect.String:
         if i+1 >= len(arguments) {
            return fmt.Errorf("flag needs an argument: %s", name)
         }
         i++
         valueField.SetString(arguments[i])
      case reflect.Int:
         if i+1 >= len(arguments) {
            return fmt.Errorf("flag needs an argument: %s", name)
         }
         i++
         parsedInt, err := strconv.ParseInt(arguments[i], 10, 64)
         if err != nil {
            return fmt.Errorf("invalid integer value %q for flag %s", arguments[i], name)
         }
         valueField.SetInt(parsedInt)
      }
   }

   return nil
}

func PrintFlags(w io.Writer, target any) error {
   value := reflect.ValueOf(target)
   if value.Kind() == reflect.Ptr {
      value = value.Elem()
   }
   if value.Kind() != reflect.Struct {
      return fmt.Errorf("target must be a struct or a pointer to a struct")
   }
   targetType := value.Type()

   for i := 0; i < targetType.NumField(); i++ {
      structField := targetType.Field(i)

      if flagTypes[structField.Type] {
         usage := value.Field(i).FieldByName("Usage").String()
         _, err := fmt.Fprintf(w, "  %s\n    \t%s\n", structField.Name, usage)
         if err != nil {
            return err
         }
      }
   }

   return nil
}

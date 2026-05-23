package myflag

import (
   "fmt"
   "io"
   "reflect"
   "strconv"
   "strings"
)

type Flag[T bool | string | int] struct {
   Set      bool
   HasEqual bool
   Value    T
   Usage    string
   Requires string
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
      arg := arguments[i]
      name, val, hasEqual := strings.Cut(arg, "=")

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
      fieldVal.FieldByName("HasEqual").SetBool(hasEqual)
      valueField := fieldVal.FieldByName("Value")

      switch valueField.Kind() {
      case reflect.Bool:
         if val == "" {
            valueField.SetBool(false)
         } else {
            parsedBool, err := strconv.ParseBool(val)
            if err != nil {
               return fmt.Errorf("invalid boolean value %q for flag %s", val, name)
            }
            valueField.SetBool(parsedBool)
         }
      case reflect.String:
         valueField.SetString(val)
      case reflect.Int:
         if val == "" {
            valueField.SetInt(0)
         } else {
            parsedInt, err := strconv.ParseInt(val, 10, 64)
            if err != nil {
               return fmt.Errorf("invalid integer value %q for flag %s", val, name)
            }
            valueField.SetInt(parsedInt)
         }
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
         fieldVal := value.Field(i)
         usage := fieldVal.FieldByName("Usage").String()

         valField := fieldVal.FieldByName("Value")
         defVal := valField.Interface()
         zeroVal := reflect.Zero(valField.Type()).Interface()

         var err error
         if defVal == zeroVal {
            _, err = fmt.Fprintf(w, "%s\n\t%s\n", structField.Name, usage)
         } else if valField.Kind() == reflect.String {
            _, err = fmt.Fprintf(w, "%s\n\t%s (default %q)\n", structField.Name, usage, defVal)
         } else {
            _, err = fmt.Fprintf(w, "%s\n\t%s (default %v)\n", structField.Name, usage, defVal)
         }

         if err != nil {
            return err
         }
      }
   }

   return nil
}

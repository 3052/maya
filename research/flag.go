package myflag

import (
   "fmt"
   "io"
   "reflect"
   "strconv"
)

type Flag[T bool | string | int] struct {
   Usage string
   Value T
   Set   bool
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

      var found bool
      for j := 0; j < targetType.NumField(); j++ {
         if targetType.Field(j).Name == name {
            found = true
            fieldVal := value.Field(j)

            setField := fieldVal.FieldByName("Set")
            valueField := fieldVal.FieldByName("Value")

            if !setField.IsValid() || !valueField.IsValid() {
               return fmt.Errorf("field %s is not a valid flag", name)
            }

            setField.SetBool(true)

            if valueField.Kind() == reflect.Bool {
               valueField.SetBool(true)
            } else if valueField.Kind() == reflect.String {
               if i+1 >= len(arguments) {
                  return fmt.Errorf("flag needs an argument: %s", name)
               }
               i++
               valueField.SetString(arguments[i])
            } else if valueField.Kind() == reflect.Int {
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
            break
         }
      }

      if !found {
         return fmt.Errorf("flag provided but not defined: %s", name)
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
      fieldVal := value.Field(i)

      if fieldVal.Kind() == reflect.Struct {
         usageField := fieldVal.FieldByName("Usage")
         if usageField.IsValid() && usageField.Kind() == reflect.String {
            _, err := fmt.Fprintf(w, "  %s\n    \t%s\n", targetType.Field(i).Name, usageField.String())
            if err != nil {
               return err
            }
         }
      }
   }

   return nil
}

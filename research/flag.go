package myflag

import (
   "fmt"
   "reflect"
)

func Parse(target any, arguments []string) error {
   v := reflect.ValueOf(target)
   if v.Kind() != reflect.Ptr || v.Elem().Kind() != reflect.Struct {
      return fmt.Errorf("target must be a pointer to a struct")
   }
   v = v.Elem()
   t := v.Type()

   for i := 0; i < len(arguments); i++ {
      name := arguments[i]

      var fieldVal reflect.Value
      var isBool bool
      var found bool

      for j := 0; j < t.NumField(); j++ {
         f := t.Field(j)
         if f.Name == name {
            fieldVal = v.Field(j)
            isBool = f.Type.Kind() == reflect.Bool
            found = true
            break
         }
      }

      if !found {
         return fmt.Errorf("flag provided but not defined: %s", name)
      }

      if isBool {
         fieldVal.SetBool(true)
      } else {
         if i+1 >= len(arguments) {
            return fmt.Errorf("flag needs an argument: %s", name)
         }
         i++
         fieldVal.SetString(arguments[i])
      }
   }

   return nil
}

func PrintDefaults(target any) {
   v := reflect.ValueOf(target)
   if v.Kind() == reflect.Ptr {
      v = v.Elem()
   }
   if v.Kind() != reflect.Struct {
      return
   }
   t := v.Type()

   for i := 0; i < t.NumField(); i++ {
      f := t.Field(i)
      usage := f.Tag.Get("usage")
      fmt.Printf("  %s\n       %s\n", f.Name, usage)
   }
}

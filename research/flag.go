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

func PrintFlags(w io.Writer, progName string, target any) error {
   value := reflect.ValueOf(target)
   if value.Kind() == reflect.Ptr {
      value = value.Elem()
   }
   if value.Kind() != reflect.Struct {
      return fmt.Errorf("target must be a struct or a pointer to a struct")
   }
   targetType := value.Type()

   var validFields []reflect.StructField
   firstLetterCounts := make(map[byte]int)

   for i := 0; i < targetType.NumField(); i++ {
      structField := targetType.Field(i)
      if flagTypes[structField.Type] {
         validFields = append(validFields, structField)
         firstLetterCounts[structField.Name[0]]++
      }
   }

   data := &strings.Builder{}

   fmt.Fprintf(data, "Index:\n")
   for _, structField := range validFields {
      fieldVal := value.FieldByName(structField.Name)
      valField := fieldVal.FieldByName("Value")
      usage := fieldVal.FieldByName("Usage").String()

      defVal := valField.Interface()
      zeroVal := reflect.Zero(valField.Type()).Interface()

      nameAndType := structField.Name
      if valField.Kind() != reflect.Bool {
         nameAndType += " " + valField.Kind().String()
      }

      fmt.Fprintf(data, "\t%s\n", nameAndType)

      if usage == "" {
         if defVal != zeroVal {
            fmt.Fprintf(data, "\t\t(default %v)\n", defVal)
         }
      } else {
         if defVal == zeroVal {
            fmt.Fprintf(data, "\t\t%s\n", usage)
         } else {
            fmt.Fprintf(data, "\t\t%s (default %v)\n", usage, defVal)
         }
      }
   }

   fmt.Fprintf(data, "\nExamples:\n")

   formatExample := func(name string) string {
      short := name
      if firstLetterCounts[name[0]] == 1 {
         short = string(name[0])
      }

      switch value.FieldByName(name).FieldByName("Value").Kind() {
      case reflect.Bool:
         return short
      case reflect.Int:
         return short + "=789"
      }
      return short + "=xyz"
   }

   for _, structField := range validFields {
      currentStr := formatExample(structField.Name)

      reqStr := ""
      requires := value.FieldByName(structField.Name).FieldByName("Requires").String()
      if requires != "" {
         reqStructField, ok := targetType.FieldByName(requires)
         if !ok {
            return fmt.Errorf("required flag %q not found", requires)
         }
         if !flagTypes[reqStructField.Type] {
            return fmt.Errorf("required flag %q is not a valid flag type", requires)
         }
         reqStr = formatExample(requires) + " "
      }

      fmt.Fprintf(data, "\t%s %s%s\n", progName, reqStr, currentStr)
   }

   _, err := fmt.Fprint(w, data)
   return err
}

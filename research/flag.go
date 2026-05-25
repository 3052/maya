package miniflag

import (
   "fmt"
   "io"
   "strconv"
   "strings"
)

type Value interface {
   Parse(string) error
   Type() string
   Default() string
}

type StringValue string

func (s *StringValue) Parse(value string) error {
   *s = StringValue(value)
   return nil
}

func (s StringValue) Type() string {
   return "string"
}

func (s StringValue) Default() string {
   return string(s)
}

type IntValue int

func (i *IntValue) Parse(value string) error {
   parsed, err := strconv.Atoi(value)
   if err != nil {
      return err
   }
   *i = IntValue(parsed)
   return nil
}

func (i IntValue) Type() string {
   return "int"
}

func (i IntValue) Default() string {
   if i != 0 {
      return strconv.Itoa(int(i))
   }
   return ""
}

type BoolValue bool

func (b *BoolValue) Parse(value string) error {
   if value == "" {
      *b = true
      return nil
   }
   parsed, err := strconv.ParseBool(value)
   if err != nil {
      return err
   }
   *b = BoolValue(parsed)
   return nil
}

func (b BoolValue) Type() string {
   return "bool"
}

func (b BoolValue) Default() string {
   if b {
      return "true"
   }
   return ""
}

type Flag struct {
   Name  string
   Usage string
   Value Value
   IsSet bool
}

type FlagSet []*Flag

func (set FlagSet) IsSet(target Value) bool {
   for _, item := range set {
      if item.Value == target {
         return item.IsSet
      }
   }
   return false
}

func (set FlagSet) Parse(args []string) error {
   for _, arg := range args {
      name, value, _ := strings.Cut(arg, "=")

      if name == "" {
         return fmt.Errorf("bad flag syntax: %s", arg)
      }

      var matched *Flag
      var matchCount int

      for _, item := range set {
         if strings.HasPrefix(item.Name, name) {
            matched = item
            matchCount++
         }
      }

      if matchCount == 0 {
         return fmt.Errorf("flag provided but not defined: %s", name)
      }
      if matchCount > 1 {
         return fmt.Errorf("ambiguous flag: %s", name)
      }

      if err := matched.Value.Parse(value); err != nil {
         return fmt.Errorf("invalid value %q for flag %s: %v", value, matched.Name, err)
      }

      matched.IsSet = true
   }

   return nil
}

func (set FlagSet) Usage(w io.Writer) error {
   data := new(strings.Builder)
   for _, item := range set {
      nameAndType := item.Name
      if typ := item.Value.Type(); typ != "" {
         nameAndType += " " + typ
      }

      def := item.Value.Default()

      if def != "" {
         if item.Usage != "" {
            fmt.Fprintf(data, "%s\n\t%s (default %s)\n", nameAndType, item.Usage, def)
         } else {
            fmt.Fprintf(data, "%s\n\t(default %s)\n", nameAndType, def)
         }
      } else if item.Usage != "" {
         fmt.Fprintf(data, "%s\n\t%s\n", nameAndType, item.Usage)
      } else {
         fmt.Fprintf(data, "%s\n", nameAndType)
      }
   }
   _, err := fmt.Fprint(w, data)
   return err
}

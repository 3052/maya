package miniflag

import (
   "fmt"
   "io"
   "strconv"
   "strings"
)

type Value interface {
   Set(string) error
   String() string
   Type() string
}

type StringValue string

func (s *StringValue) Set(value string) error {
   *s = StringValue(value)
   return nil
}

func (s StringValue) String() string {
   return string(s)
}

func (s StringValue) Type() string {
   return "string"
}

type IntValue int

func (i *IntValue) Set(value string) error {
   parsed, err := strconv.Atoi(value)
   if err != nil {
      return err
   }
   *i = IntValue(parsed)
   return nil
}

func (i IntValue) String() string {
   return strconv.Itoa(int(i))
}

func (i IntValue) Type() string {
   return "int"
}

type BoolValue bool

func (b *BoolValue) Set(value string) error {
   // If the flag is provided without an equal sign (e.g. "verbose"),
   // value will be "". Default it to true.
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

func (b BoolValue) String() string {
   return strconv.FormatBool(bool(b))
}

func (b BoolValue) Type() string {
   return "bool"
}

type Flag struct {
   Name  string
   Usage string
   Value Value
   IsSet bool
}

type FlagSet []*Flag

func (set FlagSet) Lookup(name string) *Flag {
   for _, item := range set {
      if item.Name == name {
         return item
      }
   }
   return nil
}

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

      matched := set.Lookup(name)
      if matched == nil {
         return fmt.Errorf("flag provided but not defined: %s", name)
      }

      if err := matched.Value.Set(value); err != nil {
         return fmt.Errorf("invalid value %q for flag %s: %v", value, name, err)
      }

      matched.IsSet = true
   }

   return nil
}

func (set FlagSet) Usage(w io.Writer) error {
   data := new(strings.Builder)
   for _, item := range set {
      if typ := item.Value.Type(); typ != "" {
         fmt.Fprintf(data, "%s %s", item.Name, typ)
      } else {
         fmt.Fprintf(data, "%s", item.Name)
      }

      if def := item.Value.String(); def != "" {
         fmt.Fprintf(data, " (default: %s)", def)
      }

      fmt.Fprintf(data, "\n\t%s\n", item.Usage)
   }
   _, err := fmt.Fprint(w, data)
   return err
}

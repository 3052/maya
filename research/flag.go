package miniflag

import (
   "fmt"
   "io"
   "strconv"
   "strings"
)

type Value interface {
   Set(string) error
   IsZero() bool
   String() string
}

type StringValue string

func (s *StringValue) Set(value string) error {
   *s = StringValue(value)
   return nil
}

func (s StringValue) IsZero() bool {
   return s == ""
}

func (s StringValue) String() string {
   return string(s)
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

func (i IntValue) IsZero() bool {
   return i == 0
}

func (i IntValue) String() string {
   return strconv.Itoa(int(i))
}

type Flag struct {
   Name       string
   Usage      string
   Value      Value
   IsSet      bool
   NeedsValue bool
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
      name, value, hasEqual := strings.Cut(arg, "=")

      if name == "" {
         return fmt.Errorf("bad flag syntax: %s", arg)
      }

      matched := set.Lookup(name)
      if matched == nil {
         return fmt.Errorf("flag provided but not defined: %s", name)
      }

      if matched.NeedsValue && !hasEqual {
         return fmt.Errorf("flag %s requires '='", name)
      }
      if !matched.NeedsValue && hasEqual {
         return fmt.Errorf("flag %s must not have '='", name)
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
      if item.NeedsValue {
         fmt.Fprintf(data, "%s value", item.Name)
      } else {
         fmt.Fprintf(data, "%s", item.Name)
      }

      if !item.Value.IsZero() {
         fmt.Fprintf(data, " (default: %s)", item.Value.String())
      }

      fmt.Fprintf(data, "\n\t%s\n", item.Usage)
   }
   _, err := fmt.Fprint(w, data)
   return err
}

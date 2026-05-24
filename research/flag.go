package flag

import (
   "fmt"
   "strconv"
   "strings"
)

type Value interface {
   string | int
}

type Flag[T Value] struct {
   Index int
   Name  string
   Usage string
   Value T
}

type FlagSet struct {
   Strings []*Flag[string]
   Ints    []*Flag[int]
}

func (f *FlagSet) AddString(name string, value string, usage string) {
   f.Strings = append(f.Strings, &Flag[string]{
      Index: len(f.Strings) + len(f.Ints),
      Name:  name,
      Usage: usage,
      Value: value,
   })
}

func (f *FlagSet) AddInt(name string, value int, usage string) {
   f.Ints = append(f.Ints, &Flag[int]{
      Index: len(f.Strings) + len(f.Ints),
      Name:  name,
      Usage: usage,
      Value: value,
   })
}

func Lookup[T Value](flags []*Flag[T], name string) *Flag[T] {
   for _, fl := range flags {
      if fl.Name == name {
         return fl
      }
   }
   return nil
}

func (f *FlagSet) Parse(args []string) error {
   for _, arg := range args {
      name, value, _ := strings.Cut(arg, "=")

      if fl := Lookup(f.Strings, name); fl != nil {
         fl.Value = value
         continue
      }

      if fl := Lookup(f.Ints, name); fl != nil {
         v, err := strconv.Atoi(value)
         if err != nil {
            return fmt.Errorf("invalid int value for %s: %v", name, err)
         }
         fl.Value = v
         continue
      }

      return fmt.Errorf("unknown flag: %s", name)
   }
   return nil
}

package flag

import (
   "fmt"
   "strconv"
   "strings"
)

type Value interface {
   string | int | bool
}

type Flag[T Value] struct {
   Name  string
   Value T
   Usage string
   Needs string
   Set   bool
   index int
}

type FlagSet struct {
   Strings []*Flag[string]
   Ints    []*Flag[int]
   Bools   []*Flag[bool]
}

func (f *FlagSet) nextIndex() int {
   return len(f.Strings) + len(f.Ints) + len(f.Bools)
}

func (f *FlagSet) String(newFlag *Flag[string]) {
   newFlag.index = f.nextIndex()
   f.Strings = append(f.Strings, newFlag)
}

func (f *FlagSet) Int(newFlag *Flag[int]) {
   newFlag.index = f.nextIndex()
   f.Ints = append(f.Ints, newFlag)
}

func (f *FlagSet) Bool(newFlag *Flag[bool]) {
   newFlag.index = f.nextIndex()
   f.Bools = append(f.Bools, newFlag)
}

func (f *FlagSet) Parse(args []string) error {
   for _, arg := range args {
      name, value, hasValue := strings.Cut(arg, "=")

      var matchedString *Flag[string]
      var matchedInt *Flag[int]
      var matchedBool *Flag[bool]
      matchCount := 0

      for _, stringFlag := range f.Strings {
         if strings.HasPrefix(stringFlag.Name, name) {
            matchedString = stringFlag
            matchCount++
         }
      }

      for _, intFlag := range f.Ints {
         if strings.HasPrefix(intFlag.Name, name) {
            matchedInt = intFlag
            matchCount++
         }
      }

      for _, boolFlag := range f.Bools {
         if strings.HasPrefix(boolFlag.Name, name) {
            matchedBool = boolFlag
            matchCount++
         }
      }

      if matchCount == 0 {
         return fmt.Errorf("unknown flag: %s", name)
      }
      if matchCount > 1 {
         return fmt.Errorf("ambiguous flag: %s", name)
      }

      if matchedString != nil {
         matchedString.Value = value
         matchedString.Set = true
      } else if matchedInt != nil {
         parsedInt, err := strconv.Atoi(value)
         if err != nil {
            return fmt.Errorf("invalid int value for %s: %v", name, err)
         }
         matchedInt.Value = parsedInt
         matchedInt.Set = true
      } else if matchedBool != nil {
         if hasValue {
            parsedBool, err := strconv.ParseBool(value)
            if err != nil {
               return fmt.Errorf("invalid bool value for %s: %v", name, err)
            }
            matchedBool.Value = parsedBool
         } else {
            matchedBool.Value = true
         }
         matchedBool.Set = true
      }
   }
   return nil
}

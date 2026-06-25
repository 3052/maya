// cli.go
package maya

import (
   "encoding/xml"
   "fmt"
   "io"
   "log"
   "os"
   "path/filepath"
   "strconv"
   "strings"
)

type Cache string

func (c Cache) Decode(values ...CacheValue) error {
   for _, value := range values {
      filename := filepath.Join(string(c), value.CachePath())

      data, err := os.ReadFile(filename)
      if err != nil {
         return err
      }

      err = xml.Unmarshal(data, value)
      if err != nil {
         return fmt.Errorf("failed to decode XML for %T: %w", value, err)
      }
   }
   return nil
}

func (c Cache) Encode(values ...CacheValue) error {
   for _, value := range values {
      data, err := xml.MarshalIndent(value, "", "  ")
      if err != nil {
         return fmt.Errorf("failed to encode XML for %T: %w", value, err)
      }

      filename := filepath.Join(string(c), value.CachePath())

      if err := os.MkdirAll(filepath.Dir(filename), os.ModePerm); err != nil {
         return fmt.Errorf("failed to create directory for %s: %w", filename, err)
      }

      log.Println("create:", filename)

      err = os.WriteFile(filename, data, os.ModePerm)
      if err != nil {
         return fmt.Errorf("failed to write file %s: %w", filename, err)
      }
   }

   return nil
}

func (c *Cache) Setup() error {
   cacheDir, err := os.UserCacheDir()
   if err != nil {
      return fmt.Errorf("failed to get cache directory: %w", err)
   }

   *c = Cache(cacheDir)

   return nil
}

type CacheValue interface {
   CachePath() string
}

type Flag struct {
   Name  string
   Usage string
   Value FlagValue
   Needs string
   isSet bool
}

type FlagBool bool

func (b FlagBool) Default() string {
   if b {
      return "true"
   }
   return ""
}

func (FlagBool) Example() string {
   return ""
}

func (b *FlagBool) Parse(value string) error {
   if value == "" {
      *b = true
      return nil
   }
   parsed, err := strconv.ParseBool(value)
   if err != nil {
      return err
   }
   *b = FlagBool(parsed)
   return nil
}

func (FlagBool) Type() string {
   return "bool"
}

type FlagInt int

func (i FlagInt) Default() string {
   if i != 0 {
      return strconv.Itoa(int(i))
   }
   return ""
}

func (FlagInt) Example() string {
   return "789"
}

func (i *FlagInt) Parse(value string) error {
   parsed, err := strconv.Atoi(value)
   if err != nil {
      return err
   }
   *i = FlagInt(parsed)
   return nil
}

func (FlagInt) Type() string {
   return "int"
}

type FlagSet []*Flag

func (set FlagSet) IsSet(value FlagValue) bool {
   for _, item := range set {
      if item.Value == value {
         return item.isSet
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
         // Validate inside the existing loop
         if item.Name == "" {
            return fmt.Errorf("flag name cannot be empty")
         }

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
         return fmt.Errorf("invalid value for flag %s: %v", matched.Name, err)
      }

      matched.isSet = true
   }

   return nil
}

func (set FlagSet) Usage(w io.Writer, name string) error {
   data := new(strings.Builder)

   // --- 1. Index Section ---
   fmt.Fprint(data, "index:\n")
   for _, item := range set {
      // Validate inside the existing loop (protects the next section from panicking)
      if item.Name == "" {
         return fmt.Errorf("flag name cannot be empty")
      }

      nameAndType := item.Name + " " + item.Value.Type()
      def := item.Value.Default()

      fmt.Fprintf(data, "     %s\n", nameAndType)

      if item.Usage != "" {
         fmt.Fprintf(data, "          usage: %s\n", item.Usage)
      }
      if def != "" {
         fmt.Fprintf(data, "          default: %s\n", def)
      }
   }

   // --- 2. Examples Section ---
   fmt.Fprint(data, "\nexamples:\n")

   formatFlag := func(f *Flag) string {
      firstLetter := f.Name[:1]
      count := 0
      for _, x := range set {
         if strings.HasPrefix(x.Name, firstLetter) {
            count++
         }
      }

      prefix := f.Name
      if count == 1 {
         prefix = firstLetter
      }

      if ex := f.Value.Example(); ex != "" {
         return prefix + "=" + ex
      }
      return prefix
   }

   for _, item := range set {
      fmt.Fprintf(data, "     %s", name)
      if item.Needs != "" {
         needed := set.lookup(item.Needs)
         if needed == nil {
            return fmt.Errorf("flag %q needs undefined flag %q", item.Name, item.Needs)
         }
         fmt.Fprintf(data, " %s", formatFlag(needed))
      }
      fmt.Fprintf(data, " %s\n", formatFlag(item))
   }

   _, err := fmt.Fprint(w, data)
   return err
}

func (set FlagSet) lookup(name string) *Flag {
   for _, item := range set {
      if item.Name == name {
         return item
      }
   }
   return nil
}

///

type FlagString string

func (s FlagString) Default() string {
   return string(s)
}

func (FlagString) Example() string {
   return "xyz"
}

func (s *FlagString) Parse(value string) error {
   *s = FlagString(value)
   return nil
}

func (FlagString) Type() string {
   return "string"
}

type FlagValue interface {
   Parse(string) error
   Type() string
   Default() string
   Example() string
}

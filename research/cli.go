package cli

import (
   "fmt"
   "net/url"
   "os"
   "strconv"
   "strings"
)

type Flag struct {
   Group    int
   Usage    string
   HasValue bool
   Set      bool
   Value    string
}

func (f *Flag) String() string {
   var builder strings.Builder
   builder.WriteString(f.Usage)
   if f.HasValue {
      builder.WriteString(" (requires value)")
   }
   return builder.String()
}

func (f *Flag) ParseInt() (int, error) {
   result, err := strconv.Atoi(f.Value)
   if err != nil {
      return 0, fmt.Errorf("invalid value %q for flag %q: %v", f.Value, f.Usage, err)
   }
   return result, nil
}

func (f *Flag) ParseUrl() (*url.URL, error) {
   result, err := url.Parse(f.Value)
   if err != nil {
      return nil, fmt.Errorf("invalid value %q for flag %q: %v", f.Value, f.Usage, err)
   }
   return result, nil
}

type FlagSet []*Flag

func (fs *FlagSet) Add(group int, hasValue bool, usage string) *Flag {
   f := &Flag{
      Group:    group,
      HasValue: hasValue,
      Usage:    usage,
   }
   *fs = append(*fs, f)
   return f
}

// Lookup returns the Flag that matches the given key within its Usage string.
// It returns an error if zero flags match, or if multiple flags match.
func (fs FlagSet) Lookup(key string) (*Flag, error) {
   var matched *Flag
   var matchCount int

   for _, f := range fs {
      if strings.Contains(f.Usage, key) {
         matched = f
         matchCount++
      }
   }

   if matchCount == 0 {
      return nil, fmt.Errorf("unknown flag: %s", key)
   }
   if matchCount > 1 {
      return nil, fmt.Errorf("ambiguous flag: %s matches multiple definitions", key)
   }

   return matched, nil
}

func (fs FlagSet) Parse() error {
   for i := 1; i < len(os.Args); i++ {
      key := os.Args[i]

      matched, err := fs.Lookup(key)
      if err != nil {
         return err
      }

      matched.Set = true

      if matched.HasValue {
         if i+1 >= len(os.Args) {
            return fmt.Errorf("flag '%s' requires a value", key)
         }
         i++
         matched.Value = os.Args[i]
      }
   }
   return nil
}

func (fs FlagSet) String() string {
   var builder strings.Builder
   builder.WriteString("Usage (provide any unique substring to match a flag):")

   var currentGroup int
   first := true

   for _, f := range fs {
      if first || f.Group != currentGroup {
         fmt.Fprintf(&builder, "\n\nGroup %d:", f.Group)
         currentGroup = f.Group
         first = false
      }
      fmt.Fprintf(&builder, "\n- %s", f)
   }

   return builder.String()
}

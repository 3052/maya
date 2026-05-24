package main

import (
   "flag"
   "fmt"
)

type hello struct {
   value string
}

func (h *hello) Set(value string) error {
   h.value = value
   return nil
}

func (h *hello) String() string {
   return h.value
}

func (h *hello) set() bool {
   var set bool
   flag.Visit(func(f *flag.Flag) {
      if f.Value == h {
         set = true
      }
   })
   return set
}

func main() {
   var alfa hello
   var bravo hello
   flag.Var(&alfa, "alfa", "usage")
   flag.Var(&bravo, "bravo", "usage")
   flag.Parse()
   fmt.Println(alfa.set(), bravo.set())
}

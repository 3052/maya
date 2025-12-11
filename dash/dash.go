package main

import (
   "41.neocities.org/net"
   "encoding/json"
   "flag"
   "io"
   "log"
   "net/http"
   "net/url"
   "os"
   "path/filepath"
)

func main() {
   log.SetFlags(log.Ltime)
   net.Transport(func(req *http.Request) string {
      return "L"
   })
   err := new(command).run()
   if err != nil {
      log.Fatal(err)
   }
}

type mpd struct {
   Body []byte
   Url  *url.URL
}

func (c *command) run() error {
   cache, err := os.UserCacheDir()
   if err != nil {
      return err
   }
   cache = filepath.ToSlash(cache)
   c.name = cache + "/dash/mpd.json"

   flag.StringVar(&c.address, "a", "", "address")
   flag.StringVar(&c.config.DecryptionKey, "k", "", "key")
   flag.StringVar(&c.representation, "r", "", "Representation ID")
   flag.Parse()

   if c.address != "" {
      return c.do_address()
   }
   if c.representation != "" {
      return c.do_representation()
   }
   flag.Usage()
   return nil
}

func (c *command) do_address() error {
   resp, err := http.Get(c.address)
   if err != nil {
      return err
   }
   defer resp.Body.Close()
   var cache mpd
   cache.Body, err = io.ReadAll(resp.Body)
   if err != nil {
      return err
   }
   cache.Url = resp.Request.URL
   data, err := json.Marshal(cache)
   if err != nil {
      return err
   }
   log.Println("WriteFile", c.name)
   err = os.WriteFile(c.name, data, os.ModePerm)
   if err != nil {
      return err
   }
   return net.Representations(cache.Url, cache.Body)
}

type command struct {
   name           string
   config         net.Config
   address        string
   representation string
}

func (c *command) do_representation() error {
   data, err := os.ReadFile(c.name)
   if err != nil {
      return err
   }
   var cache mpd
   err = json.Unmarshal(data, &cache)
   if err != nil {
      return err
   }
   return c.config.Download(cache.Url, cache.Body, c.representation)
}

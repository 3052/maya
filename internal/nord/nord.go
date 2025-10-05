package main

import (
   "41.neocities.org/net/nord"
   "errors"
   "flag"
   "fmt"
   "io"
   "log"
   "net/http"
   "net/url"
   "os"
   "os/exec"
   "path/filepath"
   "time"
)

func command(name string, arg ...string) ([]byte, error) {
   c := exec.Command(name, arg...)
   log.Println("Output", c.Args)
   return c.Output()
}

func do_country(name, code string) error {
   data, err := read_file(name)
   if err != nil {
      return err
   }
   var loads nord.ServerLoads
   err = loads.Unmarshal(data)
   if err != nil {
      return err
   }
   country, ok := loads.Country(code)
   if !ok {
      return errors.New(".Country")
   }
   data, err = loads.Marshal()
   if err != nil {
      return err
   }
   err = write_file(name, data)
   if err != nil {
      return err
   }
   user, err := command("credential", "-h", "api.nordvpn.com", "-k", "user")
   if err != nil {
      return err
   }
   password, err := command("credential", "-h", "api.nordvpn.com")
   if err != nil {
      return err
   }
   fmt.Println(nord.Proxy(string(user), string(password), country))
   return nil
}
func do_write(name string) error {
   servers, err := nord.GetServers(0)
   if err != nil {
      return err
   }
   data, err := nord.GetServerLoads(servers).Marshal()
   if err != nil {
      return err
   }
   return write_file(name, data)
}

func main() {
   http.DefaultTransport = &http.Transport{
      Proxy: func(req *http.Request) (*url.URL, error) {
         log.Println(req.Method, req.URL)
         return http.ProxyFromEnvironment(req)
      },
   }
   log.SetFlags(log.Ltime)
   write := flag.Bool("w", false, "write")
   country_code := flag.String("c", "", "country code")
   flag.Parse()
   name, err := os.UserHomeDir()
   if err != nil {
      panic(err)
   }
   name = filepath.ToSlash(name) + "/net/nord/ServerLoads"
   switch {
   case *country_code != "":
      err = do_country(name, *country_code)
   case *write:
      err = do_write(name)
   default:
      flag.Usage()
   }
   if err != nil {
      panic(err)
   }
}

func write_file(name string, data []byte) error {
   log.Println("WriteFile", name)
   return os.WriteFile(name, data, os.ModePerm)
}

func read_file(name string) ([]byte, error) {
   file, err := os.Open(name)
   if err != nil {
      return nil, err
   }
   defer file.Close()
   info, err := file.Stat()
   if err != nil {
      return nil, err
   }
   const month = 30 * 24 * time.Hour
   if time.Since(info.ModTime()) >= month {
      return nil, errors.New("ModTime")
   }
   return io.ReadAll(file)
}

package maya

import (
   "encoding/xml"
   "log"
   "os"
)

type Cache struct {
   file string
}

func (c *Cache) Read(value any) error {
   data, err := os.ReadFile(c.file)
   if err != nil {
      return err
   }
   return xml.Unmarshal(data, value)
}

func (c *Cache) Write(value any) error {
   data, err := xml.Marshal(value)
   if err != nil {
      return err
   }
   log.Println("Write:", c.file)
   return os.WriteFile(c.file, data, os.ModePerm)
}

func (c *Cache) Update(value any, update func() error) error {
   if err := c.Read(value); err != nil {
      return err
   }
   // The callback is NOT optional; if it fails, we return the error
   if err := update(); err != nil {
      return err
   }
   // The Write is NOT optional; if it fails, we return the error
   return c.Write(value)
}

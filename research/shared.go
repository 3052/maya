package maya

import (
   "encoding/xml"
   "os"
)

type Cache struct {
   path string
}

func (c *Cache) Read(value any) error {
   data, err := os.ReadFile(c.path)
   if err != nil {
      return err
   }
   return xml.Unmarshal(data, value)
}

func (c *Cache) TryRead(value any) error {
   err := c.Read(value)
   if os.IsNotExist(err) {
      return nil
   }
   return err
}

func (c *Cache) Write(value any) error {
   data, err := xml.Marshal(value)
   if err != nil {
      return err
   }
   return os.WriteFile(c.path, data, os.ModePerm)
}

func (c *Cache) Update(value any, fn func() error) error {
   if err := c.Read(value); err != nil {
      return err
   }
   if err := fn(); err != nil {
      return err
   }
   return c.Write(value)
}

func (c *Cache) TryUpdate(value any, fn func() error) error {
   if err := c.TryRead(value); err != nil {
      return err
   }
   if err := fn(); err != nil {
      return err
   }
   return c.Write(value)
}

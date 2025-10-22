package net

import (
   "41.neocities.org/dash"
   "41.neocities.org/sofia/file"
   "errors"
   "net/http"
)

func (c *Config) segment_base(represent *dash.Representation) error {
   if Threads != 1 {
      return errors.New("SegmentBase Threads")
   }
   var media media_file
   err := media.New(represent)
   if err != nil {
      return err
   }
   os_file, err := create(represent)
   if err != nil {
      return err
   }
   defer os_file.Close()
   head := http.Header{}
   head.Set("range", "bytes="+represent.SegmentBase.Initialization.Range)
   data, err := get_segment(represent.BaseUrl[0], head)
   if err != nil {
      return err
   }
   data, err = media.initialization(data)
   if err != nil {
      return err
   }
   _, err = os_file.Write(data)
   if err != nil {
      return err
   }
   key, err := c.key(&media)
   if err != nil {
      return err
   }
   head.Set("range", "bytes="+represent.SegmentBase.IndexRange)
   data, err = get_segment(represent.BaseUrl[0], head)
   if err != nil {
      return err
   }
   var file_file file.File
   err = file_file.Read(data)
   if err != nil {
      return err
   }
   var progressVar progress
   progressVar.set(len(file_file.Sidx.Reference))
   var index index_range
   err = index.Set(represent.SegmentBase.IndexRange)
   if err != nil {
      return err
   }
   for _, reference := range file_file.Sidx.Reference {
      index[0] = index[1] + 1
      index[1] += uint64(reference.Size())
      head.Set("range", "bytes="+index.String())
      data, err = get_segment(represent.BaseUrl[0], head)
      if err != nil {
         return err
      }
      progressVar.next()
      data, err = media.write_segment(data, key)
      if err != nil {
         return err
      }
      _, err = os_file.Write(data)
      if err != nil {
         return err
      }
   }
   return nil
}

package net

import (
   "encoding/hex"
   "errors"
   "fmt"
   "net/http"
   "net/url"
)

var (
   ErrMissingTraf = errors.New("missing traf box")
   ErrKeyMismatch = errors.New("key ID mismatch")
)

const widevineURN = "urn:uuid:edef8ba9-79d6-4ace-a3c8-27dcd51d21ed"

var widevineID, _ = hex.DecodeString("edef8ba979d64acea3c827dcd51d21ed")

// indexRange handles the byte range parsing for SegmentBase
type indexRange [2]uint64

func (i *indexRange) Set(data string) error {
   _, err := fmt.Sscanf(data, "%v-%v", &i[0], &i[1])
   return err
}

func (i *indexRange) String() string {
   return fmt.Sprintf("%v-%v", i[0], i[1])
}

// Internal types for the worker pool
type mediaRequest struct {
   url    *url.URL
   header http.Header
}

type job struct {
   index   int
   request mediaRequest
}

type result struct {
   index int
   data  []byte
   err   error
}

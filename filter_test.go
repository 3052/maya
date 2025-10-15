package net

import (
   "41.neocities.org/dash"
   "slices"
   "testing"
)

func point[T any](data T) *T {
   return &data
}

func TestFind(t *testing.T) {
   streams := []*dash.Representation{
      {Id: "v12", Bandwidth: 4618234, Codecs: point("hvc1.2.4.L120.90"), Height: point(1080)}, // Below target
      {Id: "v16", Bandwidth: 4965335, Codecs: point("hvc1.2.4.L120.90"), Height: point(1080)}, // Below target
      {Id: "v19", Bandwidth: 24155623, Codecs: point("hvc1.2.4.L150.90"), Height: point(2160)},// Wrong height
      {Id: "v26", Bandwidth: 3189767, Codecs: point("dvh1.05.03"), Height: point(900)},    // Wrong height
      {Id: "v5", Bandwidth: 11206136, Codecs: point("avc1.640028"), Height: point(1080)},   // Wrong codec (avc1)
      {Id: "v8", Bandwidth: 8355097, Codecs: point("hvc1.2.4.L120.90"), Height: point(1080)},  // Above target
   }
   expectedID := "v8"
   result := find(slices.Values(streams), &Filter{Bandwidth: 7_500_000})
   if result == nil {
      t.Fatal("FindGoodStream returned a nil result without an error.")
   }
   if result.Id != expectedID {
      t.Errorf("Incorrect stream selected. Expected Id '%s', but got '%s'", expectedID, result.Id)
   }
}

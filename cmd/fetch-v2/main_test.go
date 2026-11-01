package main

import (
	"testing"

	"github.com/retroplasma/flyover-reverse-engineering/pkg/mps"
)

func TestPickZoom(t *testing.T) {
	r3d := []mps.TileRange{{MaxX: 2047, MaxY: 2047, MinZ: 11, MaxZ: 20}}
	meta := []mps.TileRange{{MaxX: 511, MaxY: 511, MinZ: 9, MaxZ: 9}, {MaxX: 8191, MaxY: 8191, MinZ: 13, MaxZ: 13}}
	for _, c := range []struct {
		ranges     []mps.TileRange
		zoom, want int
	}{
		{r3d, 18, 18}, {r3d, 22, 20}, {r3d, 5, 11},
		{meta, 18, 13}, {meta, 8, 9}, {meta, 12, 13},
	} {
		if got, ok := pickZoom(c.ranges, c.zoom); !ok || got != c.want {
			t.Errorf("pickZoom(%v, %d) = %d, want %d", c.ranges, c.zoom, got, c.want)
		}
	}
	if _, ok := pickZoom(nil, 10); ok {
		t.Error("empty ranges should fail")
	}
	if !inRanges(r3d, 18, 44000, 104000) || inRanges(r3d, 21, 0, 0) {
		t.Error("inRanges")
	}
}

func TestMagic(t *testing.T) {
	if got := magic([]byte("R3D\x01\x00")); got != "5233440100 R3D.." {
		t.Errorf("magic = %q", got)
	}
	if got := magic(nil); got != " " {
		t.Errorf("magic(nil) = %q", got)
	}
}

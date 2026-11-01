package mps

import (
	"io/ioutil"
	"os"
	"reflect"
	"testing"

	"github.com/golang/protobuf/proto"
)

func varint(v uint64) []byte {
	var b []byte
	for v >= 0x80 {
		b = append(b, byte(v)|0x80)
		v >>= 7
	}
	return append(b, byte(v))
}

func field(f int, payload []byte) []byte {
	return append(append(varint(uint64(f)<<3|2), varint(uint64(len(payload)))...), payload...)
}

func vfield(f int, v uint64) []byte {
	return append(varint(uint64(f)<<3), varint(v)...)
}

func rng(vals ...uint64) []byte {
	var b []byte
	for i, v := range vals {
		b = append(b, vfield(i+1, v)...)
	}
	return b
}

func TestStyleVersion(t *testing.T) {
	ver := append(vfield(1, 758), field(2, rng(0, 0, 8191, 8191, 13, 13))...)
	ver = append(field(2, rng(0, 0, 511, 511, 9, 9)), ver...)
	sc := append(field(1, []byte("https://example.invalid/tile")), vfield(3, 100)...)
	sc = append(sc, field(5, ver)...)
	sc = append(sc, vfield(7, 2)...)
	raw := field(2, sc)

	var rm ResourceManifest
	if err := proto.Unmarshal(raw, &rm); err != nil {
		t.Fatal(err)
	}
	cfg, err := rm.StyleConfigByID(StyleFlyoverV2Metadata)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.UrlPrefix_1 != "https://example.invalid/tile" {
		t.Errorf("prefix %q", cfg.UrlPrefix_1)
	}
	sv, err := cfg.Version()
	if err != nil {
		t.Fatal(err)
	}
	want := StyleVersion{758, []TileRange{{0, 0, 511, 511, 9, 9}, {0, 0, 8191, 8191, 13, 13}}}
	if !reflect.DeepEqual(sv, want) {
		t.Errorf("got %+v, want %+v", sv, want)
	}
	if !want.Ranges[1].Contains(13, 100, 200) || want.Ranges[0].Contains(13, 100, 200) {
		t.Error("Contains")
	}
	// rectangle is at MinZ and scales to deeper zooms
	r := TileRange{MinX: 10, MinY: 0, MaxX: 11, MaxY: 2047, MinZ: 11, MaxZ: 20}
	for _, c := range []struct {
		z, x, y int
		want    bool
	}{
		{11, 10, 0, true}, {11, 12, 0, false}, {12, 20, 0, true}, {12, 23, 4095, true},
		{12, 24, 0, false}, {12, 19, 0, false}, {20, 10 << 9, 0, true}, {21, 10 << 10, 0, false},
	} {
		if got := r.Contains(c.z, c.x, c.y); got != c.want {
			t.Errorf("Contains(%d,%d,%d) = %v", c.z, c.x, c.y, got)
		}
	}
}

// Set FLYOVER_MANIFEST to a downloaded ResourceManifest.pbd to check real data
func TestStyleVersionRealManifest(t *testing.T) {
	path := os.Getenv("FLYOVER_MANIFEST")
	if path == "" {
		t.Skip("FLYOVER_MANIFEST not set")
	}
	raw, err := ioutil.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var rm ResourceManifest
	if err := proto.Unmarshal(raw, &rm); err != nil {
		t.Fatal(err)
	}
	for _, id := range []ResourceManifest_StyleConfig_StyleID{StyleFlyoverV2R3D, StyleFlyoverV2DSM, StyleFlyoverV2Metadata} {
		cfg, err := rm.StyleConfigByID(id)
		if err != nil {
			t.Fatal(err)
		}
		sv, err := cfg.Version()
		if err != nil {
			t.Fatal(id, err)
		}
		t.Logf("style %d: %s v=%d ranges=%+v", id, cfg.UrlPrefix_1, sv.Version, sv.Ranges)
	}
}

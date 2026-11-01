package r3d

import (
	"bytes"
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"testing"
)

func uv(v uint64) []byte {
	b := make([]byte, binary.MaxVarintLen64)
	return b[:binary.PutUvarint(b, v)]
}

func tlv(tag uint64, payload []byte) []byte {
	return append(append(uv(tag), uv(uint64(len(payload)))...), payload...)
}

func node(header []byte, entries ...[]byte) []byte {
	b := append(append([]byte{}, nodePrefix...), uv(uint64(len(header)))...)
	b = append(b, header...)
	for _, e := range entries {
		b = append(b, e...)
	}
	return b
}

func transformHeader(extra int) []byte {
	h := make([]byte, 96+extra)
	vals := []float64{1, 0, 0, 0, 1, 0, 0, 0, 1, -2525468.25, -4651404.5, 3547152.75}
	for i, v := range vals {
		binary.LittleEndian.PutUint64(h[8*i:], math.Float64bits(v))
	}
	return h
}

func heif(brand string, payload []byte) []byte {
	ftyp := append([]byte{0, 0, 0, 16}, []byte("ftyp"+brand+"\x00\x00\x00\x00")...)
	meta := append([]byte{0, 0, 0, 12}, []byte("metaXXXX")...)
	mdat := append([]byte{0, 0, 0, byte(8 + len(payload))}, []byte("mdat")...)
	return append(append(append(ftyp, meta...), mdat...), payload...)
}

func f32le(v float32) []byte {
	b := make([]byte, 4)
	binary.LittleEndian.PutUint32(b, math.Float32bits(v))
	return b
}

func buildTile(count uint64, splatPayload []byte) []byte {
	steps := []float32{0.16, 0.04, 0.01, 0.01, 0.01, 0.05, 0.05, 0.05, 0.05}
	if count == 0 {
		steps = steps[:1] // empty blocks carry a single step
	}
	splat := make([]byte, 0, 64+len(splatPayload))
	splat = binary.LittleEndian.AppendUint32(splat, SplatMagic)
	splat = binary.LittleEndian.AppendUint32(splat, 1)
	splat = binary.LittleEndian.AppendUint64(splat, count)
	splat = binary.LittleEndian.AppendUint32(splat, uint32(len(steps)-1))
	for _, s := range steps {
		splat = append(splat, f32le(s)...)
	}
	splat = append(splat, splatPayload...)

	var bbox []byte
	for _, v := range []float32{-0.8, 0.1, -0.5, 0.8, 0.8, 0.8} {
		bbox = append(bbox, f32le(v)...)
	}
	textures := append(append([]byte{0x01, 0xa9, 0xa3}, heif("heic", []byte("rgb"))...), heif("heix", []byte("grey1"))...)
	textures = append(textures, heif("heix", []byte("grey2"))...)
	mesh := node(transformHeader(56), tlv(2, []byte{0xe1, 0x21, 0x66, 0x24, 9, 9}), tlv(3, textures))
	top := node(transformHeader(10),
		tlv(4, bbox),
		tlv(2, []byte{5, 1, 1, 1, 2, 1, 3, 7, 1, 1, 6, 1, 2, 4, 1, 1}),
		tlv(3, splat),
		tlv(2, mesh),
		tlv(3, []byte{1, 1, 1, 0}))
	b := append(append([]byte{}, FileMagic...), 2, 0, 0, 0, 1, 2, 0, 1)
	b = append(b, uv(uint64(len(splat)+160))...)
	return append(b, top...)
}

func TestParseSynthetic(t *testing.T) {
	payload := []byte("compressed-splat-data")
	data := buildTile(1234, payload)
	tile, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	if tile.Transform.Translation[0] != -2525468.25 || tile.Transform.Rotation[4] != 1 {
		t.Errorf("transform %+v", tile.Transform)
	}
	if tile.BoundingBox == nil || tile.BoundingBox.HalfSize[2] != 0.8 {
		t.Errorf("bbox %+v", tile.BoundingBox)
	}
	if len(tile.Attributes) != 16 || tile.Attributes[0] != 5 {
		t.Errorf("attributes %x", tile.Attributes)
	}
	s := tile.Splats
	if s == nil || s.Count != 1234 || s.Version != 1 || len(s.Steps) != 9 || s.Steps[8] != 0.05 || s.HeaderSize != 56 {
		t.Fatalf("splats %+v", s)
	}
	if !bytes.Equal(s.Payload(), payload) || !bytes.Equal(data[s.Offset:s.Offset+len(s.Raw)], s.Raw) {
		t.Error("splat block bytes/offset")
	}
	if !bytes.HasPrefix(tile.Mesh, []byte{0xe1, 0x21, 0x66, 0x24}) {
		t.Errorf("mesh %x", tile.Mesh)
	}
	if len(tile.Textures) != 3 || tile.Textures[0].Brand != "heic" || tile.Textures[2].Brand != "heix" {
		t.Fatalf("textures %+v", tile.Textures)
	}
	for _, tx := range tile.Textures {
		if !bytes.Equal(data[tx.Offset:tx.Offset+len(tx.Data)], tx.Data) || string(tx.Data[4:8]) != "ftyp" {
			t.Error("texture bytes/offset")
		}
	}
}

func TestParseEmptySplats(t *testing.T) {
	tile, err := Parse(buildTile(0, nil))
	if err != nil {
		t.Fatal(err)
	}
	if tile.Splats == nil || tile.Splats.Count != 0 || len(tile.Splats.Payload()) != 0 || tile.Splats.HeaderSize != 24 {
		t.Errorf("splats %+v", tile.Splats)
	}
}

func TestParseRejects(t *testing.T) {
	if _, err := Parse([]byte("C3M\x03 not r3d")); err == nil {
		t.Error("accepted bad magic")
	}
	data := buildTile(1, []byte("x"))
	if _, err := Parse(data[:len(data)-3]); err == nil {
		t.Error("accepted truncated tile")
	}
}

// Set R3D_SAMPLES to a directory of downloaded style-98 .bin tiles to check real data
func TestParseSamples(t *testing.T) {
	dir := os.Getenv("R3D_SAMPLES")
	if dir == "" {
		t.Skip("R3D_SAMPLES not set")
	}
	files, _ := filepath.Glob(filepath.Join(dir, "*.bin"))
	if len(files) == 0 {
		t.Fatal("no .bin files")
	}
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		tile, err := Parse(data)
		if err != nil {
			t.Errorf("%s: %v", filepath.Base(f), err)
			continue
		}
		if tile.Splats == nil || len(tile.Mesh) == 0 || len(tile.Textures) != 3 {
			t.Errorf("%s: incomplete parse (splats=%v mesh=%d textures=%d)", filepath.Base(f), tile.Splats != nil, len(tile.Mesh), len(tile.Textures))
			continue
		}
		t.Logf("%s: splats=%d steps=%v (%d B) mesh=%d B textures=%d", filepath.Base(f), tile.Splats.Count, tile.Splats.Steps, len(tile.Splats.Raw), len(tile.Mesh), len(tile.Textures))
	}
}

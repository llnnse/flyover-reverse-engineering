package r3d

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestExtract(t *testing.T) {
	payload := []byte("compressed-splat-data")
	data := buildTile(42, payload)
	dir := filepath.Join(t.TempDir(), "18_1_2")
	res, err := Extract(data, "src.bin", dir, false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Splats != 42 || res.Textures != 3 || res.MeshBytes == 0 {
		t.Errorf("result %+v", res)
	}
	tile, _ := Parse(data)
	for file, want := range map[string][]byte{
		"splats.bin":     tile.Splats.Raw,
		"mesh.bin":       tile.Mesh,
		"texture_0.heic": tile.Textures[0].Data,
		"texture_2.heic": tile.Textures[2].Data,
	} {
		got, err := os.ReadFile(filepath.Join(dir, file))
		if err != nil || !bytes.Equal(got, want) {
			t.Errorf("%s: %v", file, err)
		}
	}
	var m extractMeta
	js, _ := os.ReadFile(filepath.Join(dir, "meta.json"))
	if err := json.Unmarshal(js, &m); err != nil {
		t.Fatal(err)
	}
	if m.Source != "src.bin" || m.Splats == nil || m.Splats.Count != 42 || len(m.Textures) != 3 || m.BoundingBox == nil {
		t.Errorf("meta %+v", m)
	}

	// splats only
	dir2 := filepath.Join(t.TempDir(), "only")
	if _, err := Extract(data, "src.bin", dir2, true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir2, "mesh.bin")); !os.IsNotExist(err) {
		t.Error("mesh.bin written with splatsOnly")
	}
	if _, err := Extract([]byte("not r3d"), "x", filepath.Join(t.TempDir(), "bad"), false); err == nil {
		t.Error("accepted invalid tile")
	}
}

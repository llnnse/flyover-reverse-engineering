package exp

import (
	"bytes"
	"io/ioutil"
	"path/filepath"
	"strings"
	"testing"

	"github.com/retroplasma/flyover-reverse-engineering/pkg/fly/c3m"
)

func TestOBJExportKeepsTextureFormats(t *testing.T) {
	jpeg := []byte{0xFF, 0xD8, 0xFF, 0xE0, 'j', 'p', 'e', 'g'}
	heic := []byte{0, 0, 0, 0x18, 'f', 't', 'y', 'p', 'h', 'e', 'i', 'c'}

	tile := c3m.C3M{
		Header: c3m.Header{Rotation: [9]float64{1, 0, 0, 0, 1, 0, 0, 0, 1}},
		Materials: []c3m.Material{
			{Format: c3m.TextureJPEG, Image: jpeg},
			{Format: c3m.TextureHEIC, Image: heic},
		},
		Meshes: []c3m.Mesh{{
			Vertices: []c3m.Vertex{{X: 0, Y: 0, Z: 0}, {X: 1, Y: 0, Z: 0}, {X: 0, Y: 1, Z: 0}},
			Groups:   map[int]c3m.Group{0: {Material: 1, Faces: []c3m.Face{{A: 0, B: 1, C: 2}}}},
		}},
	}

	dir := t.TempDir()
	e, err := New(dir, "t_")
	if err != nil {
		t.Fatal(err)
	}
	if err := e.Next(tile, "0"); err != nil {
		t.Fatal(err)
	}
	if err := e.Close(); err != nil {
		t.Fatal(err)
	}

	for name, want := range map[string][]byte{"t_0_0.jpg": jpeg, "t_0_1.heic": heic} {
		got, err := ioutil.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if !bytes.Equal(got, want) {
			t.Errorf("%s: texture was modified", name)
		}
	}

	mtl, err := ioutil.ReadFile(filepath.Join(dir, "t_model.mtl"))
	if err != nil {
		t.Fatal(err)
	}
	for _, ref := range []string{"map_Kd t_0_0.jpg", "map_Kd t_0_1.heic"} {
		if !strings.Contains(string(mtl), ref) {
			t.Errorf("mtl missing %q:\n%s", ref, mtl)
		}
	}

	obj, err := ioutil.ReadFile(filepath.Join(dir, "t_model.obj"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"vn 0 0 1\n", "f 1/1/1 2/2/2 3/3/3\n"} {
		if !strings.Contains(string(obj), want) {
			t.Errorf("obj missing %q:\n%s", want, obj)
		}
	}
}

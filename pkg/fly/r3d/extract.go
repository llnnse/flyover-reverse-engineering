package r3d

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// ExtractResult summarizes what Extract wrote
type ExtractResult struct {
	Dir        string
	Splats     uint64 // splat count
	SplatBytes int
	MeshBytes  int
	Textures   int
}

type extractMeta struct {
	Source      string            `json:"source"`
	Version     uint32            `json:"version"`
	Rotation    [9]float64        `json:"rotation"`
	Translation [3]float64        `json:"translation_ecef_m"`
	BoundingBox *BoundingBox      `json:"bounding_box,omitempty"`
	Attributes  string            `json:"attributes_hex"`
	Splats      *splatMeta        `json:"splats,omitempty"`
	Mesh        *blobMeta         `json:"mesh,omitempty"`
	Textures    []textureMeta     `json:"textures"`
	Notes       map[string]string `json:"notes"`
}

type splatMeta struct {
	Count       uint64    `json:"count"`
	Version     uint32    `json:"version"`
	Steps       []float32 `json:"steps"`
	HeaderBytes int       `json:"header_bytes"`
	File        string    `json:"file"`
	Bytes       int       `json:"bytes"`
	Offset      int       `json:"offset_in_tile"`
}

type blobMeta struct {
	File  string `json:"file"`
	Bytes int    `json:"bytes"`
	Magic string `json:"magic_hex"`
}

type textureMeta struct {
	File   string `json:"file"`
	Brand  string `json:"brand"`
	Bytes  int    `json:"bytes"`
	Offset int    `json:"offset_in_tile"`
}

var extractNotes = map[string]string{
	"splats.bin":  "raw splat block as stored in the tile, including its header (magic 0x07c922d1); payload codec unknown",
	"steps":       "believed quantization steps: position grid step in meters, scalar, RGB, rotation quaternion",
	"mesh.bin":    "raw mesh geometry block; codec unknown",
	"texture_N":   "HEIF images as stored (0: RGB albedo atlas, 1-2: greyscale material maps)",
	"translation": "rotation is row-major 3x3; translation is ECEF in meters",
}

// Extract parses an R3D tile and writes its payloads unmodified to dir:
// splats.bin, mesh.bin, texture_N.heic and meta.json. With splatsOnly, the
// mesh and textures are skipped. source is recorded in meta.json.
func Extract(data []byte, source, dir string, splatsOnly bool) (res ExtractResult, err error) {
	tile, err := Parse(data)
	if err != nil {
		return res, err
	}
	if err = os.MkdirAll(dir, 0755); err != nil {
		return res, err
	}
	res.Dir = dir
	write := func(file string, b []byte) error {
		return os.WriteFile(filepath.Join(dir, file), b, 0644)
	}

	m := extractMeta{
		Source:      source,
		Version:     tile.Version,
		Rotation:    tile.Transform.Rotation,
		Translation: tile.Transform.Translation,
		BoundingBox: tile.BoundingBox,
		Attributes:  fmt.Sprintf("%x", tile.Attributes),
		Textures:    []textureMeta{},
		Notes:       extractNotes,
	}
	if s := tile.Splats; s != nil {
		if err = write("splats.bin", s.Raw); err != nil {
			return res, err
		}
		res.Splats, res.SplatBytes = s.Count, len(s.Raw)
		m.Splats = &splatMeta{Count: s.Count, Version: s.Version, Steps: s.Steps, HeaderBytes: s.HeaderSize,
			File: "splats.bin", Bytes: len(s.Raw), Offset: s.Offset}
	}
	if !splatsOnly {
		if len(tile.Mesh) > 0 {
			if err = write("mesh.bin", tile.Mesh); err != nil {
				return res, err
			}
			res.MeshBytes = len(tile.Mesh)
			m.Mesh = &blobMeta{File: "mesh.bin", Bytes: len(tile.Mesh), Magic: fmt.Sprintf("%x", tile.Mesh[:min(4, len(tile.Mesh))])}
		}
		for i, tx := range tile.Textures {
			file := fmt.Sprintf("texture_%d.heic", i)
			if err = write(file, tx.Data); err != nil {
				return res, err
			}
			m.Textures = append(m.Textures, textureMeta{File: file, Brand: tx.Brand, Bytes: len(tx.Data), Offset: tx.Offset})
		}
		res.Textures = len(tile.Textures)
	}
	js, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return res, err
	}
	err = write("meta.json", append(js, '\n'))
	return res, err
}

func (r ExtractResult) String() string {
	return fmt.Sprintf("%d splats (%d B), mesh %d B, %d textures -> %s", r.Splats, r.SplatBytes, r.MeshBytes, r.Textures, r.Dir)
}

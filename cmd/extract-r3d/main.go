// extract-r3d pulls the raw payloads out of Flyover V2 R3D tiles (style 98),
// as downloaded by cmd/fetch-v2. For each tile it writes the Gaussian-splat
// block, the mesh geometry block and the embedded HEIF textures unmodified,
// plus a meta.json with the decoded header fields.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/retroplasma/flyover-reverse-engineering/pkg/fly/r3d"
)

var l = log.New(os.Stderr, "", 0)

type meta struct {
	Source      string            `json:"source"`
	Version     uint32            `json:"version"`
	Rotation    [9]float64        `json:"rotation"`
	Translation [3]float64        `json:"translation_ecef_m"`
	BoundingBox *r3d.BoundingBox  `json:"bounding_box,omitempty"`
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

func usage() {
	l.Println("Usage:", os.Args[0], "[--out dir] [--splats-only] path ...")
	l.Println()
	l.Println("  path   R3D tile (.bin) or a directory containing them")
	l.Println()
	flag.PrintDefaults()
	l.Println()
	l.Println("Example:", os.Args[0], "downloaded_files/v2/98_r3d")
	os.Exit(1)
}

func main() {
	out := flag.String("out", "./downloaded_files/r3d_extracted", "output directory")
	splatsOnly := flag.Bool("splats-only", false, "only write splat blocks (and meta.json)")
	flag.Usage = usage
	flag.Parse()
	if flag.NArg() == 0 {
		usage()
	}

	var files []string
	for _, p := range flag.Args() {
		fi, err := os.Stat(p)
		if err != nil {
			l.Fatalln(err)
		}
		if fi.IsDir() {
			m, err := filepath.Glob(filepath.Join(p, "*.bin"))
			if err != nil {
				l.Fatalln(err)
			}
			files = append(files, m...)
		} else {
			files = append(files, p)
		}
	}
	sort.Strings(files)
	if len(files) == 0 {
		l.Fatalln("no .bin files found")
	}

	failed, splats, total := 0, 0, uint64(0)
	for _, f := range files {
		n, err := extract(f, *out, *splatsOnly)
		if err != nil {
			failed++
			l.Printf("%s: %v", f, err)
			continue
		}
		if n > 0 {
			splats++
			total += n
		}
	}
	l.Printf("%d tile(s) processed, %d with splats (%d splats total), %d failed", len(files)-failed, splats, total, failed)
	if failed > 0 {
		os.Exit(1)
	}
}

// extract writes the payloads of one tile and returns its splat count
func extract(path, out string, splatsOnly bool) (uint64, error) {
	data, err := ioutil.ReadFile(path)
	if err != nil {
		return 0, err
	}
	tile, err := r3d.Parse(data)
	if err != nil {
		return 0, err
	}
	name := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	dir := filepath.Join(out, name)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return 0, err
	}
	write := func(file string, b []byte) error {
		return ioutil.WriteFile(filepath.Join(dir, file), b, 0644)
	}

	m := meta{
		Source:      path,
		Version:     tile.Version,
		Rotation:    tile.Transform.Rotation,
		Translation: tile.Transform.Translation,
		BoundingBox: tile.BoundingBox,
		Attributes:  fmt.Sprintf("%x", tile.Attributes),
		Textures:    []textureMeta{},
		Notes: map[string]string{
			"splats.bin":  "raw splat block as stored in the tile, including its header (magic 0x07c922d1); payload codec unknown",
			"steps":       "believed quantization steps: position grid step in meters, scalar, RGB, rotation quaternion",
			"mesh.bin":    "raw mesh geometry block; codec unknown",
			"texture_N":   "HEIF images as stored (0: RGB albedo atlas, 1-2: greyscale material maps)",
			"translation": "rotation is row-major 3x3; translation is ECEF in meters",
		},
	}

	var count uint64
	if s := tile.Splats; s != nil {
		count = s.Count
		if err := write("splats.bin", s.Raw); err != nil {
			return 0, err
		}
		m.Splats = &splatMeta{Count: s.Count, Version: s.Version, Steps: s.Steps, HeaderBytes: s.HeaderSize,
			File: "splats.bin", Bytes: len(s.Raw), Offset: s.Offset}
	}
	if !splatsOnly {
		if len(tile.Mesh) > 0 {
			if err := write("mesh.bin", tile.Mesh); err != nil {
				return 0, err
			}
			m.Mesh = &blobMeta{File: "mesh.bin", Bytes: len(tile.Mesh), Magic: fmt.Sprintf("%x", tile.Mesh[:min(4, len(tile.Mesh))])}
		}
		for i, tx := range tile.Textures {
			file := fmt.Sprintf("texture_%d.heic", i)
			if err := write(file, tx.Data); err != nil {
				return 0, err
			}
			m.Textures = append(m.Textures, textureMeta{File: file, Brand: tx.Brand, Bytes: len(tx.Data), Offset: tx.Offset})
		}
	}
	js, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return 0, err
	}
	if err := write("meta.json", append(js, '\n')); err != nil {
		return 0, err
	}
	l.Printf("%s: %d splats (%d B), mesh %d B, %d textures -> %s", name, count, splatBytes(tile), len(tile.Mesh), len(tile.Textures), dir)
	return count, nil
}

func splatBytes(t *r3d.Tile) int {
	if t.Splats == nil {
		return 0
	}
	return len(t.Splats.Raw)
}

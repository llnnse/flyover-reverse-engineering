// Package r3d parses the container of Flyover V2 R3D tiles (tile style 98).
//
// The tile is a tree of tag–length–value entries with LEB128 varints. It holds a
// Gaussian-splat block and a child node with a textured mesh. The splat and mesh
// payloads use codecs that are not decoded here; they are returned raw.
// See FLYOVER_V2_NOTES.md for the layout.
package r3d

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
)

// Magic values
var (
	FileMagic  = []byte{0x3d, 0x96, 0x59, 0x2e}
	nodePrefix = []byte{0x01, 0x01, 0x01, 0x00, 0x01}
	SplatMagic = uint32(0x07c922d1)
)

// splat block header: magic, version, u64 count, u32 n, then 1+n × f32
const splatFixedHeader = 24

// Tile is a parsed R3D tile. Byte slices point into the input data.
type Tile struct {
	Version     uint32
	Transform   Transform // from the top node header
	BoundingBox *BoundingBox
	Attributes  []byte      // top-level attribute list (tag 2); {0} when there are no splats
	Splats      *SplatBlock // nil if the tile has no splat block
	Mesh        []byte      // raw mesh geometry block (child node, tag 2)
	Textures    []Texture   // embedded HEIF images, in file order
}

// Transform places the tile: rotation (row-major 3×3) and ECEF translation in meters
type Transform struct {
	Rotation    [9]float64
	Translation [3]float64
}

// BoundingBox is the top-level tag 4 entry
type BoundingBox struct {
	Center   [3]float32 `json:"center"`
	HalfSize [3]float32 `json:"half_size"`
}

// SplatBlock is the top-level tag 3 entry: a Gaussian-splat payload
type SplatBlock struct {
	Raw     []byte // whole block, including its header
	Offset  int    // offset of Raw in the tile
	Version uint32
	Count   uint64 // number of splats
	// Steps are believed to be quantization steps. Tiles with splats have nine,
	// grouped 1/1/3/4: position grid step in meters (halves with each zoom
	// level: 0.32 at z17, 0.16 at z18), a scalar, RGB, rotation quaternion.
	// Empty tiles have only the position step. The header stores
	// len(Steps)-1 before them.
	Steps      []float32
	HeaderSize int // bytes before the compressed payload
}

// Payload returns the compressed per-splat data after the block header
func (s *SplatBlock) Payload() []byte {
	return s.Raw[s.HeaderSize:]
}

// Texture is an embedded HEIF image
type Texture struct {
	Brand  string // ISO-BMFF major brand, e.g. "heic" or "heix"
	Data   []byte
	Offset int // offset of Data in the tile
}

type entry struct {
	tag    uint64
	offset int // payload offset in the tile
	data   []byte
}

// Parse parses an R3D tile
func Parse(data []byte) (*Tile, error) {
	if len(data) < 13 || !bytes.Equal(data[:4], FileMagic) {
		return nil, errors.New("not an R3D tile (bad magic)")
	}
	t := &Tile{Version: binary.LittleEndian.Uint32(data[4:8])}
	if t.Version != 2 {
		return nil, fmt.Errorf("unsupported R3D version %d", t.Version)
	}
	// bytes 8..11 are unknown (01 02 00 01), followed by a varint (≈ splat block size)
	_, o, err := uvarint(data, 12)
	if err != nil {
		return nil, err
	}
	header, entries, err := parseNode(data, o, len(data))
	if err != nil {
		return nil, fmt.Errorf("top node: %w", err)
	}
	if t.Transform, err = parseTransform(header); err != nil {
		return nil, err
	}

	for _, e := range entries {
		switch {
		case e.tag == 4 && len(e.data) == 24:
			bb := &BoundingBox{}
			for i := 0; i < 3; i++ {
				bb.Center[i] = f32(e.data, 4*i)
				bb.HalfSize[i] = f32(e.data, 12+4*i)
			}
			t.BoundingBox = bb
		case e.tag == 2 && bytes.HasPrefix(e.data, nodePrefix):
			if err := t.parseMeshNode(data, e); err != nil {
				return nil, fmt.Errorf("mesh node: %w", err)
			}
		case e.tag == 2 && t.Attributes == nil:
			t.Attributes = e.data
		case e.tag == 3 && len(e.data) >= splatFixedHeader && binary.LittleEndian.Uint32(e.data) == SplatMagic:
			s, err := parseSplatBlock(e)
			if err != nil {
				return nil, err
			}
			t.Splats = s
		}
	}
	return t, nil
}

func parseSplatBlock(e entry) (*SplatBlock, error) {
	d := e.data
	extra := binary.LittleEndian.Uint32(d[16:])
	if extra > 64 || len(d) < splatFixedHeader+4*int(extra) {
		return nil, fmt.Errorf("splat block header: %d extra steps in %d bytes", extra, len(d))
	}
	s := &SplatBlock{
		Raw:        d,
		Offset:     e.offset,
		Version:    binary.LittleEndian.Uint32(d[4:]),
		Count:      binary.LittleEndian.Uint64(d[8:]),
		Steps:      make([]float32, 1+extra),
		HeaderSize: splatFixedHeader + 4*int(extra),
	}
	for i := range s.Steps {
		s.Steps[i] = f32(d, 20+4*i)
	}
	return s, nil
}

func (t *Tile) parseMeshNode(data []byte, e entry) error {
	_, entries, err := parseNode(data, e.offset, e.offset+len(e.data))
	if err != nil {
		return err
	}
	for _, c := range entries {
		switch c.tag {
		case 2:
			if t.Mesh == nil {
				t.Mesh = c.data
			}
		case 3:
			t.Textures = append(t.Textures, findHEIF(c.data, c.offset)...)
		}
	}
	return nil
}

// parseNode parses a node starting at o: 5-byte prefix, varint-sized header,
// then tag–length–value entries up to end
func parseNode(data []byte, o, end int) (header []byte, entries []entry, err error) {
	if end > len(data) || o+len(nodePrefix) > end || !bytes.Equal(data[o:o+len(nodePrefix)], nodePrefix) {
		return nil, nil, errors.New("missing node prefix")
	}
	o += len(nodePrefix)
	n, o, err := uvarint(data, o)
	if err != nil {
		return nil, nil, err
	}
	if n > uint64(end-o) {
		return nil, nil, errors.New("node header overruns")
	}
	header = data[o : o+int(n)]
	o += int(n)
	for o < end {
		var tag, l uint64
		start := o
		if tag, o, err = uvarint(data, o); err != nil {
			return
		}
		if l, o, err = uvarint(data, o); err != nil {
			return
		}
		if l > uint64(end-o) {
			return nil, nil, fmt.Errorf("entry at %#x (tag %d, len %d) overruns", start, tag, l)
		}
		entries = append(entries, entry{tag: tag, offset: o, data: data[o : o+int(l)]})
		o += int(l)
	}
	return
}

func parseTransform(h []byte) (tr Transform, err error) {
	if len(h) < 96 {
		return tr, fmt.Errorf("node header too short (%d bytes)", len(h))
	}
	for i := 0; i < 9; i++ {
		tr.Rotation[i] = f64(h, 8*i)
	}
	for i := 0; i < 3; i++ {
		tr.Translation[i] = f64(h, 72+8*i)
	}
	return
}

// findHEIF finds ISO-BMFF images (ftyp … mdat) in b
func findHEIF(b []byte, base int) (out []Texture) {
	for i := 0; i+12 <= len(b); {
		k := bytes.Index(b[i:], []byte("ftyp"))
		if k < 0 {
			break
		}
		s := i + k - 4
		if s < 0 {
			i += k + 4
			continue
		}
		n := boxesLen(b[s:])
		if n == 0 {
			i += k + 4
			continue
		}
		out = append(out, Texture{Brand: string(b[s+8 : s+12]), Data: b[s : s+n], Offset: base + s})
		i = s + n
	}
	return
}

// boxesLen returns the length of consecutive ISO-BMFF boxes up to and including mdat
func boxesLen(b []byte) int {
	o := 0
	for o+8 <= len(b) {
		size := uint64(binary.BigEndian.Uint32(b[o:]))
		typ := string(b[o+4 : o+8])
		if size == 1 && o+16 <= len(b) {
			size = binary.BigEndian.Uint64(b[o+8:])
		}
		if size < 8 || size > uint64(len(b)-o) {
			break
		}
		o += int(size)
		if typ == "mdat" {
			return o
		}
	}
	return 0
}

func uvarint(b []byte, o int) (uint64, int, error) {
	if o >= len(b) {
		return 0, o, errors.New("truncated varint")
	}
	v, n := binary.Uvarint(b[o:])
	if n <= 0 {
		return 0, o, errors.New("bad varint")
	}
	return v, o + n, nil
}

func f32(b []byte, o int) float32 { return math.Float32frombits(binary.LittleEndian.Uint32(b[o:])) }
func f64(b []byte, o int) float64 { return math.Float64frombits(binary.LittleEndian.Uint64(b[o:])) }

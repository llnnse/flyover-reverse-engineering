package mps

import (
	"errors"
	"fmt"
)

// Flyover V2 tile styles (see FLYOVER_V2_NOTES.md)
const (
	StyleFlyoverV2R3D      ResourceManifest_StyleConfig_StyleID = 98
	StyleFlyoverV2DSM      ResourceManifest_StyleConfig_StyleID = 99
	StyleFlyoverV2Metadata ResourceManifest_StyleConfig_StyleID = 100
)

// TileRange is a rectangle of tiles available over a span of zoom levels.
// The rectangle is given in tile coordinates at MinZ.
type TileRange struct {
	MinX, MinY, MaxX, MaxY, MinZ, MaxZ int
}

// Contains reports whether the tile lies in the range
func (r TileRange) Contains(z, x, y int) bool {
	if z < r.MinZ || z > r.MaxZ {
		return false
	}
	s := uint(z - r.MinZ)
	return x >= r.MinX<<s && x < (r.MaxX+1)<<s && y >= r.MinY<<s && y < (r.MaxY+1)<<s
}

// StyleVersion is the version and coverage of a style, from field 5 of
// StyleConfig, which the generated code doesn't know about:
// 5 { 1: version, 2: { 1: minX, 2: minY, 3: maxX, 4: maxY, 5: minZ, 6: maxZ } ... }
type StyleVersion struct {
	Version int
	Ranges  []TileRange
}

// StyleConfigByID returns the style config for a style ID
func (rm ResourceManifest) StyleConfigByID(id ResourceManifest_StyleConfig_StyleID) (*ResourceManifest_StyleConfig, error) {
	for _, v := range rm.StyleConfig {
		if v.StyleId == id {
			return v, nil
		}
	}
	return nil, fmt.Errorf("style %d not in resource manifest", id)
}

// Version decodes the version and tile ranges of a style config
func (sc *ResourceManifest_StyleConfig) Version() (sv StyleVersion, err error) {
	found := false
	err = walk(sc.XXX_unrecognized, func(field int, varint uint64, bytes []byte) error {
		if field != 5 || bytes == nil {
			return nil
		}
		found = true
		return walk(bytes, func(f int, v uint64, b []byte) error {
			switch {
			case f == 1 && b == nil:
				sv.Version = int(v)
			case f == 2 && b != nil:
				var r TileRange
				vals := []*int{&r.MinX, &r.MinY, &r.MaxX, &r.MaxY, &r.MinZ, &r.MaxZ}
				if err := walk(b, func(f int, v uint64, b []byte) error {
					if f >= 1 && f <= 6 && b == nil {
						*vals[f-1] = int(v)
					}
					return nil
				}); err != nil {
					return err
				}
				sv.Ranges = append(sv.Ranges, r)
			}
			return nil
		})
	})
	if err == nil && !found {
		err = errors.New("style config has no version field")
	}
	return
}

var errTruncated = errors.New("truncated protobuf")

// walk calls fn for each field in a protobuf message: varints get v,
// length-delimited fields get b (non-nil), fixed32/64 are skipped
func walk(data []byte, fn func(field int, v uint64, b []byte) error) error {
	uvarint := func(i int) (uint64, int, error) {
		var r uint64
		for s := uint(0); s < 64; s += 7 {
			if i >= len(data) {
				return 0, 0, errTruncated
			}
			c := data[i]
			i++
			r |= uint64(c&0x7f) << s
			if c < 0x80 {
				return r, i, nil
			}
		}
		return 0, 0, errors.New("varint overflow")
	}
	for i := 0; i < len(data); {
		key, n, err := uvarint(i)
		if err != nil {
			return err
		}
		i = n
		field, wire := int(key>>3), key&7
		switch wire {
		case 0:
			v, n, err := uvarint(i)
			if err != nil {
				return err
			}
			i = n
			if err := fn(field, v, nil); err != nil {
				return err
			}
		case 2:
			l, n, err := uvarint(i)
			if err != nil {
				return err
			}
			if uint64(len(data)-n) < l {
				return errTruncated
			}
			i = n + int(l)
			if err := fn(field, 0, data[n:i:i]); err != nil {
				return err
			}
		case 1:
			i += 8
		case 5:
			i += 4
		default:
			return fmt.Errorf("unsupported wire type %d", wire)
		}
		if i > len(data) {
			return errTruncated
		}
	}
	return nil
}

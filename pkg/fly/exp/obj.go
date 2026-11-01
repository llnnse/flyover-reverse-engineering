package exp

import (
	"bufio"
	"errors"
	"fmt"
	"io/ioutil"
	"math"
	"os"
	"path"
	"runtime/debug"

	"github.com/retroplasma/flyover-reverse-engineering/pkg/fly/c3m"
	"github.com/retroplasma/flyover-reverse-engineering/pkg/oth"
)

var transform = true

type Export interface {
	Next(c3m c3m.C3M, subPfx string) error
	Close() error
}

func New(dir string, fnPfx string) (Export, error) {
	objWriter, err := newWriter(path.Join(dir, fmt.Sprintf("%smodel.obj", fnPfx)))
	if err != nil {
		return nil, err
	}
	mtlWriter, err := newWriter(path.Join(dir, fmt.Sprintf("%smodel.mtl", fnPfx)))
	if err != nil {
		return nil, err
	}
	return &OBJExport{
		dir:       dir,
		fnPfx:     fnPfx,
		vtxCount:  0,
		objWriter: objWriter,
		mtlWriter: mtlWriter,
	}, nil
}

func (e *OBJExport) Close() (err error) {
	if err = e.objWriter.done(); err != nil {
		return
	}
	if err = e.mtlWriter.done(); err != nil {
		return
	}
	return
}

type OBJExport struct {
	dir       string
	fnPfx     string
	vtxCount  int
	objWriter writer
	mtlWriter writer
}

func (e *OBJExport) Next(c3m c3m.C3M, subPfx string) (err error) {
	defer func() {
		if e := recover(); e != nil {
			err = errors.New(fmt.Sprintln(e, string(debug.Stack())))
		}
	}()

	dir, fnPfx := e.dir, e.fnPfx

	for i, material := range c3m.Materials {
		// textures are written as received (JPEG or HEIC), without re-encoding
		texFile := fmt.Sprintf("%s%s_%d.%s", fnPfx, subPfx, i, material.Format.Extension())
		oth.CheckPanic(ioutil.WriteFile(path.Join(dir, texFile), material.Image, 0644))
		nxt := fmt.Sprintf(`
newmtl mtl_%s_%d
Kd 1.000 1.000 1.000
d 1.0
illum 0
map_Kd %s
`, subPfx, i, texFile)
		e.mtlWriter.write(nxt)
	}

	for i, mesh := range c3m.Meshes {
		e.objWriter.write(fmt.Sprintf("mtllib %smodel.mtl\n", fnPfx))
		e.objWriter.write(fmt.Sprintf("o test_%s_%d\n", subPfx, i))
		transformed := make([][3]float64, len(mesh.Vertices))
		for idx, vtx := range mesh.Vertices {
			x, y, z := float64(vtx.X), float64(vtx.Y), float64(vtx.Z)
			if transform {
				x, y, z =
					c3m.Header.Rotation[0]*x+c3m.Header.Rotation[1]*y+c3m.Header.Rotation[2]*z,
					c3m.Header.Rotation[3]*x+c3m.Header.Rotation[4]*y+c3m.Header.Rotation[5]*z,
					c3m.Header.Rotation[6]*x+c3m.Header.Rotation[7]*y+c3m.Header.Rotation[8]*z
				x += c3m.Header.Translation[0]
				y += c3m.Header.Translation[1]
				z += c3m.Header.Translation[2]
			}
			transformed[idx] = [3]float64{x, y, z}
			e.objWriter.write(fmt.Sprintln("v", x, y, z))
			e.objWriter.write(fmt.Sprintln("vt", vtx.U, vtx.V))
		}

		normals := make([][3]float64, len(mesh.Vertices))
		for _, group := range mesh.Groups {
			for _, face := range group.Faces {
				if int(face.A) < len(transformed) && int(face.B) < len(transformed) && int(face.C) < len(transformed) {
					va := transformed[face.A]
					vb := transformed[face.B]
					vc := transformed[face.C]
					ab := [3]float64{vb[0] - va[0], vb[1] - va[1], vb[2] - va[2]}
					ac := [3]float64{vc[0] - va[0], vc[1] - va[1], vc[2] - va[2]}
					fn := [3]float64{
						ab[1]*ac[2] - ab[2]*ac[1],
						ab[2]*ac[0] - ab[0]*ac[2],
						ab[0]*ac[1] - ab[1]*ac[0],
					}
					normals[face.A][0] += fn[0]
					normals[face.A][1] += fn[1]
					normals[face.A][2] += fn[2]
					normals[face.B][0] += fn[0]
					normals[face.B][1] += fn[1]
					normals[face.B][2] += fn[2]
					normals[face.C][0] += fn[0]
					normals[face.C][1] += fn[1]
					normals[face.C][2] += fn[2]
				}
			}
		}
		for _, n := range normals {
			l := math.Sqrt(n[0]*n[0] + n[1]*n[1] + n[2]*n[2])
			if l > 1e-6 {
				e.objWriter.write(fmt.Sprintln("vn", n[0]/l, n[1]/l, n[2]/l))
			} else {
				e.objWriter.write("vn 0.0 0.0 1.0\n")
			}
		}

		for i, group := range mesh.Groups {
			e.objWriter.write(fmt.Sprintf("g g_%s_%d\n", subPfx, i))
			e.objWriter.write("s 1\n")
			e.objWriter.write(fmt.Sprintf("usemtl mtl_%s_%d\n", subPfx, i))
			for _, face := range group.Faces {
				a, b, c := int(face.A)+1+e.vtxCount, int(face.B)+1+e.vtxCount, int(face.C)+1+e.vtxCount
				e.objWriter.write(fmt.Sprintf("f %d/%d/%d %d/%d/%d %d/%d/%d\n", a, a, a, b, b, b, c, c, c))
			}
		}
		e.vtxCount += len(mesh.Vertices)
	}
	return
}

func create(fn string) (*os.File, error) {
	perm := os.O_CREATE | os.O_WRONLY | os.O_TRUNC
	return os.OpenFile(fn, perm, 0655)
}

func (w *writer) write(txt string) {
	_, err := w.writer.WriteString(txt)
	oth.CheckPanic(err)
}

type writer struct {
	file   *os.File
	writer *bufio.Writer
}

func newWriter(fn string) (writer, error) {
	f, err := create(fn)
	if err != nil {
		return writer{}, err
	}
	w := bufio.NewWriter(f)
	return writer{file: f, writer: w}, nil
}

func (w writer) done() (err error) {
	if err = w.writer.Flush(); err != nil {
		return
	}
	if err = w.file.Close(); err != nil {
		return
	}
	return
}

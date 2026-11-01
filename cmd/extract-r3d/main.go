// extract-r3d pulls the raw payloads out of Flyover V2 R3D tiles (style 98)
// that are already on disk. For each tile it writes the Gaussian-splat block,
// the mesh geometry block and the embedded HEIF textures unmodified, plus a
// meta.json with the decoded header fields.
//
// cmd/fetch-v2 does this automatically for tiles it downloads.
package main

import (
	"flag"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/retroplasma/flyover-reverse-engineering/pkg/fly/r3d"
)

var l = log.New(os.Stderr, "", 0)

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

	failed, withSplats, total := 0, 0, uint64(0)
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err == nil {
			name := strings.TrimSuffix(filepath.Base(f), filepath.Ext(f))
			var res r3d.ExtractResult
			if res, err = r3d.Extract(data, f, filepath.Join(*out, name), *splatsOnly); err == nil {
				l.Printf("%s: %v", name, res)
				if res.Splats > 0 {
					withSplats++
					total += res.Splats
				}
				continue
			}
		}
		failed++
		l.Printf("%s: %v", f, err)
	}
	l.Printf("%d tile(s) processed, %d with splats (%d splats total), %d failed", len(files)-failed, withSplats, total, failed)
	if failed > 0 {
		os.Exit(1)
	}
}

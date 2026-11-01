// fetch-v2 downloads raw Flyover V2 tiles (styles 98 R3D, 99 DSM, 100 metadata)
// around a location and saves them unmodified for format analysis.
//
// URLs are ?style=S&v=V&x=X&y=Y&z=Z, with the version and tile ranges taken
// from the resource manifest. R3D tiles also need h (height index); h=0 is
// added unless --query sets it. Downloaded R3D tiles are extracted right away
// (splat block, mesh block, textures, meta.json), like cmd/extract-r3d.
package main

import (
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/retroplasma/flyover-reverse-engineering/pkg/fly/r3d"
	"github.com/retroplasma/flyover-reverse-engineering/pkg/mps"
	"github.com/retroplasma/flyover-reverse-engineering/pkg/mps/config"
	"github.com/retroplasma/flyover-reverse-engineering/pkg/mth"
	"github.com/retroplasma/flyover-reverse-engineering/pkg/web"
)

var l = log.New(os.Stderr, "", 0)

var styleNames = map[mps.ResourceManifest_StyleConfig_StyleID]string{
	mps.StyleFlyoverV2R3D:      "r3d",
	mps.StyleFlyoverV2DSM:      "dsm",
	mps.StyleFlyoverV2Metadata: "metadata",
}

func usage() {
	l.Println("Usage:", os.Args[0], "[flags] lat lon zoom [radius]")
	l.Println()
	l.Println("  zoom    preferred zoom; clamped to each style's available zooms")
	l.Println("  radius  tiles around the center in each direction (default 0)")
	l.Println()
	flag.PrintDefaults()
	l.Println()
	l.Println("Example:", os.Args[0], "--styles=98 34.007603 -118.499741 18 1")
	os.Exit(1)
}

func main() {
	stylesFlag := flag.String("styles", "98,99,100", "comma-separated style IDs")
	queryFlag := flag.String("query", "", "extra query parameters appended to each URL, e.g. 'h=1'")
	versionFlag := flag.Int("version", 0, "override the version from the resource manifest")
	outFlag := flag.String("out", "./downloaded_files/v2", "output directory")
	extractFlag := flag.Bool("extract", true, "extract splats, mesh and textures from downloaded R3D tiles")
	flag.Usage = usage
	flag.Parse()
	args := flag.Args()
	if len(args) < 3 || len(args) > 4 {
		usage()
	}
	lat, err1 := strconv.ParseFloat(args[0], 64)
	lon, err2 := strconv.ParseFloat(args[1], 64)
	zoom, err3 := strconv.Atoi(args[2])
	radius := 0
	var err4 error
	if len(args) == 4 {
		radius, err4 = strconv.Atoi(args[3])
	}
	if err := errors.Join(err1, err2, err3, err4); err != nil || radius < 0 {
		l.Println("Invalid arguments:", err)
		usage()
	}
	var styles []mps.ResourceManifest_StyleConfig_StyleID
	for _, s := range strings.Split(*stylesFlag, ",") {
		id, err := strconv.Atoi(strings.TrimSpace(s))
		if err != nil {
			l.Fatalln("Invalid style:", s)
		}
		styles = append(styles, mps.ResourceManifest_StyleConfig_StyleID(id))
	}

	cache := mps.Cache{Enabled: true, Directory: "./cache"}
	check(cache.Init())
	cfg, err := config.FromJSONFile("./config.json")
	check(err)
	if !cfg.IsValid() {
		l.Fatalln("please set values in config.json")
	}
	ctx, err := mps.Init(cache, cfg)
	check(err)

	opts := options{version: *versionFlag, query: *queryFlag, out: *outFlag, extract: *extractFlag}
	failures := 0
	var sum summary
	for _, style := range styles {
		failures += fetchStyle(ctx, style, lat, lon, zoom, radius, opts, &sum)
	}
	if sum.tiles > 0 {
		l.Printf("R3D: %d tile(s) extracted, %d with splats (%d splats total) -> %s", sum.tiles, sum.withSplats, sum.splats, filepath.Join(opts.out, "98_r3d_extracted"))
	}
	if failures > 0 {
		l.Println(failures, "request(s) failed")
		os.Exit(1)
	}
}

type options struct {
	version int
	query   string
	out     string
	extract bool
}

type summary struct {
	tiles, withSplats int
	splats            uint64
}

// queryFor returns the extra query parameters for a style: R3D tiles need h,
// so h=0 is added unless the user set it
func queryFor(style mps.ResourceManifest_StyleConfig_StyleID, query string) string {
	query = strings.TrimPrefix(query, "&")
	if style != mps.StyleFlyoverV2R3D {
		return query
	}
	for _, kv := range strings.Split(query, "&") {
		if strings.HasPrefix(kv, "h=") {
			return query
		}
	}
	if query == "" {
		return "h=0"
	}
	return query + "&h=0"
}

// fetchStyle downloads the tiles of one style and returns the number of failures
func fetchStyle(ctx mps.Context, style mps.ResourceManifest_StyleConfig_StyleID, lat, lon float64, zoom, radius int, opts options, sum *summary) (failures int) {
	version, query, out := opts.version, queryFor(style, opts.query), opts.out
	name := styleNames[style]
	if name == "" {
		name = "unknown"
	}
	sc, err := ctx.ResourceManifest.StyleConfigByID(style)
	if err != nil {
		l.Println("Style", style, "skipped:", err)
		return 1
	}
	sv, err := sc.Version()
	if err != nil {
		l.Println("Style", style, "skipped:", err)
		return 1
	}
	if version != 0 {
		sv.Version = version
	}
	l.Printf("Style %d (%s): %s v=%d ranges=%+v", style, name, sc.UrlPrefix_1, sv.Version, sv.Ranges)

	z, ok := pickZoom(sv.Ranges, zoom)
	if !ok {
		l.Println("Style", style, "skipped: no tile ranges")
		return 1
	}
	if z != zoom {
		l.Printf("  using zoom %d (requested %d)", z, zoom)
	}

	dir := filepath.Join(out, fmt.Sprintf("%d_%s", style, name))
	check(os.MkdirAll(dir, 0755))
	index, err := os.OpenFile(filepath.Join(dir, "index.tsv"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	check(err)
	defer index.Close()

	sep := "?"
	if strings.Contains(sc.UrlPrefix_1, "?") {
		sep = "&"
	}
	cx, cy := mth.LatLonToTileOSM(z, lat, lon)
	for dy := -radius; dy <= radius; dy++ {
		for dx := -radius; dx <= radius; dx++ {
			x, y := cx+dx, cy+dy
			if !inRanges(sv.Ranges, z, x, y) {
				continue
			}
			url := fmt.Sprintf("%s%sstyle=%d&v=%d&x=%d&y=%d&z=%d", sc.UrlPrefix_1, sep, style, sv.Version, x, y, z)
			if query != "" {
				url += "&" + query
			}
			data, ctype, err := fetch(ctx, url)
			status := "200"
			var se web.StatusError
			if errors.As(err, &se) {
				status = strconv.Itoa(se.Code)
			} else if err != nil {
				status = "error"
			}
			// the logged URL is the unsigned one, so no session or signature is written
			fmt.Fprintf(index, "%d\t%d\t%d\t%s\t%s\t%d\t%s\t%s\n", z, x, y, status, ctype, len(data), magic(data), url)

			switch {
			case web.IsAuthError(err):
				l.Fatalln("Authentication rejected by server (", err, "). Regenerate config.json.")
			case web.IsNotFound(err):
				l.Printf("  z=%d x=%d y=%d: not found", z, x, y)
			case err != nil:
				failures++
				l.Printf("  z=%d x=%d y=%d: %v", z, x, y, err)
			default:
				name := fmt.Sprintf("%d_%d_%d", z, x, y)
				fn := filepath.Join(dir, name+".bin")
				check(ioutil.WriteFile(fn, data, 0644))
				l.Printf("  z=%d x=%d y=%d: %d bytes, %s, magic %s -> %s", z, x, y, len(data), ctype, magic(data), fn)
				if opts.extract && style == mps.StyleFlyoverV2R3D {
					res, err := r3d.Extract(data, fn, filepath.Join(out, "98_r3d_extracted", name), false)
					if err != nil {
						failures++
						l.Printf("    extract: %v", err)
						continue
					}
					l.Printf("    extracted: %v", res)
					sum.tiles++
					if res.Splats > 0 {
						sum.withSplats++
						sum.splats += res.Splats
					}
				}
			}
		}
	}
	return
}

// pickZoom returns the available zoom closest to the requested one
func pickZoom(ranges []mps.TileRange, zoom int) (int, bool) {
	best, ok := 0, false
	for _, r := range ranges {
		z := zoom
		if z < r.MinZ {
			z = r.MinZ
		} else if z > r.MaxZ {
			z = r.MaxZ
		}
		if !ok || abs(z-zoom) < abs(best-zoom) {
			best, ok = z, true
		}
	}
	return best, ok
}

func inRanges(ranges []mps.TileRange, z, x, y int) bool {
	for _, r := range ranges {
		if r.Contains(z, x, y) {
			return true
		}
	}
	return false
}

func fetch(ctx mps.Context, url string) (data []byte, ctype string, err error) {
	authURL, err := ctx.AuthContext.AuthURL(url)
	if err != nil {
		return nil, "", err
	}
	data, err = web.GetWithCheck(authURL, func(res *http.Response) error {
		ctype = res.Header.Get("content-type")
		return nil
	})
	return
}

// magic returns the first bytes as hex and printable ASCII, to identify formats
func magic(data []byte) string {
	n := len(data)
	if n > 16 {
		n = 16
	}
	ascii := []byte(string(data[:n]))
	for i, c := range ascii {
		if c < 0x20 || c > 0x7e {
			ascii[i] = '.'
		}
	}
	return hex.EncodeToString(data[:n]) + " " + string(ascii)
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func check(err error) {
	if err != nil {
		l.Fatalln(err)
	}
}

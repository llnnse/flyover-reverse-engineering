package main

import (
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"math"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/retroplasma/flyover-reverse-engineering/pkg/fly"
	"github.com/retroplasma/flyover-reverse-engineering/pkg/fly/c3m"
	"github.com/retroplasma/flyover-reverse-engineering/pkg/fly/c3mm"
	"github.com/retroplasma/flyover-reverse-engineering/pkg/fly/exp"
	"github.com/retroplasma/flyover-reverse-engineering/pkg/mps"
	"github.com/retroplasma/flyover-reverse-engineering/pkg/mps/config"
	"github.com/retroplasma/flyover-reverse-engineering/pkg/mth"
	"github.com/retroplasma/flyover-reverse-engineering/pkg/oth"
	"github.com/retroplasma/flyover-reverse-engineering/pkg/web"
)

var l = log.New(os.Stderr, "", 0)

func printUsage(msg string) {
	if msg != "" {
		l.Println("Error:", msg)
	}
	l.Println("Usage", os.Args[0], "[lat] [lon] [zoom] [tryXY] [tryH] [[--parallel]]")
	l.Println()
	l.Println("  Name    Description       Example")
	l.Println("  --------------------------------------")
	ex := []string{"34.007603", "-118.499741", "20", "3", "40"}
	l.Println("  lat     Latitude         ", ex[0])
	l.Println("  lon     Longitude        ", ex[1])
	l.Println("  zoom    Zoom (~ 13-20)   ", ex[2])
	l.Println("  tryXY   Area scan        ", ex[3])
	l.Println("  tryH    Altitude scan    ", ex[4])
	l.Println("Example:", os.Args[0], ex[0], ex[1], ex[2], ex[3], ex[4])
	os.Exit(1)
}

func main() {

	var err error
	aReq := make([]string, 0)
	aOpt := make([]string, 0)
	for _, a := range os.Args[1:] {
		if !strings.HasPrefix(a, "--") {
			aReq = append(aReq, a)
		} else {
			aOpt = append(aOpt, a)
		}
	}
	if len(os.Args) == 1 {
		printUsage("")
	}
	if len(aReq) != 5 {
		printUsage("Invalid argument number")
	}
	lat, err := strconv.ParseFloat(aReq[0], 64)
	if err != nil {
		printUsage("Invalid lat")
	}
	lon, err := strconv.ParseFloat(aReq[1], 64)
	if err != nil {
		printUsage("Invalid lon")
	}
	zoom, err := strconv.ParseInt(aReq[2], 10, 32)
	if err != nil {
		printUsage("Invalid zoom")
	}
	tryXY, err := strconv.ParseInt(aReq[3], 10, 32)
	if err != nil {
		printUsage("Invalid tryXY")
	}
	tryH, err := strconv.ParseInt(aReq[4], 10, 32)
	if err != nil {
		printUsage("Invalid tryH")
	}
	parallel := false
	for _, a := range aOpt {
		switch a {
		case "--parallel":
			parallel = true
		default:
			printUsage("Unknown param: " + a)
		}
	}

	cache := mps.Cache{Enabled: true, Directory: "./cache"}
	err = cache.Init()
	oth.CheckPanic(err)
	config, err := config.FromJSONFile("./config.json")
	oth.CheckPanic(err)
	if !config.IsValid() {
		fmt.Fprintln(os.Stderr, "please set values in config.json")
		os.Exit(1)
	}
	ctx, err := getContext(cache, config)
	oth.CheckPanic(err)

	z := int(zoom)
	x, y := mth.LatLonToTileTMS(z, lat, lon)

	p, err := ctx.findPlace(lat, lon)
	oth.CheckPanic(err)
	l.Println(p.Name, p.Radius, p.Lat, p.Lon)

	err = os.MkdirAll(fmt.Sprintf("./cache/c3mm/%d_%d", p.Region, p.Version), 0755)
	oth.CheckPanic(err)

	exportDir := fmt.Sprintf("./downloaded_files/obj/%f-%f-%d-%d-%d", lat, lon, zoom, tryXY, tryH)
	err = os.MkdirAll(exportDir, 0755)
	oth.CheckPanic(err)

	xp := 0
	export, err := exp.New(exportDir, "exp_")
	oth.CheckPanic(err)

	c3m.DisableLogs()

	// semaphore settings
	dln := 8
	if parallel {
		dln = 16
	}
	sem := make(chan int, dln)
	var wg sync.WaitGroup

	// exporter for decoded tiles
	ex, exDone := make(chan c3m.C3M, dln), make(chan int)
	go func() {
		for tile := range ex {
			oth.CheckPanic(export.Next(tile, fmt.Sprintf("%d", xp)))
			xp++
		}
		exDone <- 1
	}()

	// report logs a failed request; rejected authentication aborts the export
	var failures int32
	report := func(err error) {
		if web.IsAuthError(err) {
			l.Fatalln("Authentication rejected by server (", err, "). Regenerate config.json.")
		}
		atomic.AddInt32(&failures, 1)
		l.Println("Error:", err)
	}

	// loop over area and altitude
	for dx := -tryXY; dx <= tryXY; dx++ {
		for dy := -tryXY; dy <= tryXY; dy++ {
			xn := x + int(dx)
			yn := y + int(dy)

			hasGround, err := ctx.checkTile(p, z, yn, xn, 0)
			if err != nil {
				report(err)
				continue
			}
			if !hasGround {
				continue
			}

			sem <- 1
			wg.Add(1)
			dx, dy := dx, dy
			go func() {
				defer wg.Done()
				defer func() { <-sem }()

				tile, err := ctx.getTile(p, z, yn, xn, 0)
				if err != nil {
					report(err)
					return
				}
				if len(tile.Meshes) == 0 {
					return
				}
				l.Println("Exporting", dx, dy, "h = 0")
				ex <- tile

				for h := 1; h < int(tryH); h++ {
					hasTile, err := ctx.checkTile(p, z, yn, xn, h)
					if err != nil {
						report(err)
						break
					}
					if !hasTile {
						break
					}
					hTile, err := ctx.getTile(p, z, yn, xn, h)
					if err != nil {
						report(err)
						break
					}
					if len(hTile.Meshes) == 0 {
						break
					}
					l.Println("Exporting", dx, dy, "h =", h)
					ex <- hTile
				}
			}()
		}
	}
	wg.Wait() // wait for all tile loads to finish
	close(ex) // no more tiles sent to exporter
	<-exDone  // wait till all tiles are exported
	oth.CheckPanic(export.Close())
	l.Println(xp, "exported")
	if n := atomic.LoadInt32(&failures); n > 0 {
		l.Println(n, "request(s) failed; the export is incomplete")
		os.Exit(1)
	}
}

func (ctx *context) checkTile(p fly.Trigger, z, y, x, h int) (bool, error) {

	tile := c3mm.Tile{Z: z, Y: y, X: x, H: h}

	c3mm0, err := ctx.getC3mm(p, 0)
	if errors.Is(err, errC3mmUnavailable) {
		// no usable C3MM (style 14) metadata; probe tiles directly
		return true, nil
	}
	if err != nil {
		return false, err
	}

	smallestZ := c3mm0.RootIndex.SmallestZ

	if tile.Z < smallestZ {
		return false, errors.New("z too small")
	}

	// list of tiles from requested to lowest level of detail
	list := make([]c3mm.Tile, 0)
	for t := tile; t.Z >= smallestZ; t = t.ZoomedOut() {
		list = append(list, t)
	}

	// find octree root
	root, listIdx := c3mm.Root{}, len(list)-1
	for ; listIdx >= 0; listIdx-- {
		t := list[listIdx]
		n := len(c3mm0.RootIndex.Entries)
		idx := sort.Search(n, func(i int) bool {
			root := c3mm0.RootIndex.Entries[i]
			return t.Less(root.Tile) || t == root.Tile
		})
		if idx == n {
			continue
		}
		root = c3mm0.RootIndex.Entries[idx]
		if t != root.Tile {
			continue
		}
		break
	}
	if listIdx < 0 {
		return false, nil
	}

	// readOctant reads an octant from c3mm files and moves the offset
	readOctant := func(octantOffset *int) (c3mm.Octant, error) {
		partNum := c3mm0.FileIndex.GetPartNumber(*octantOffset)
		c3mm1, err := ctx.getC3mm(p, partNum)
		if err != nil {
			return c3mm.Octant{}, err
		}
		return c3mm1.GetOctant(octantOffset, c3mm0.FileIndex.Entries[partNum]), nil
	}

	rootOffset := root.Offset
	octant, err := readOctant(&rootOffset)
	if err != nil {
		return false, err
	}

	if list[listIdx] == tile {
		return true, nil
	}
	if listIdx == 0 {
		return false, nil
	}

	// traverse octree
	for ; octant.Next > 0; listIdx-- {
		zoomedInActual := list[listIdx-1]
		zoomedInCandidates := list[listIdx].ZoomedInCandidates()
		bits := octant.Bits
		octantOffset := octant.Next
		matched := false
		for o := 0; o < 8; o++ {
			if (bits>>(o*2))&1 != 1 {
				continue
			}
			octant, err = readOctant(&octantOffset)
			if err != nil {
				return false, err
			}
			if zoomedInCandidates(o) == zoomedInActual {
				matched = true
				break
			}
		}
		if !matched {
			return false, nil
		}
		if tile == zoomedInActual {
			return true, nil
		}
	}
	return false, nil
}

func (ctx *context) getTile(p fly.Trigger, z, y, x, h int) (c3m.C3M, error) {
	yn := mth.TileCountPerAxis(z) - 1 - y // invert y
	url := fmt.Sprintf("%s?style=%d&v=%d&region=%d&x=%d&y=%d&z=%d&h=%d",
		ctx.URLPrefixC3m, mps.ResourceManifest_StyleConfig_C3M, p.Version, p.Region, x, yn, z, h)

	data, err := ctx.get(url)
	if isMissing(err) || (err == nil && len(data) == 0) {
		return c3m.C3M{}, nil
	}
	if err != nil {
		return c3m.C3M{}, fmt.Errorf("tile z=%d x=%d y=%d h=%d: %w", z, x, y, h, err)
	}
	tile, err := c3m.Parse(data)
	if err != nil {
		return c3m.C3M{}, fmt.Errorf("parse tile z=%d x=%d y=%d h=%d: %w", z, x, y, h, err)
	}
	return tile, nil
}

var errC3mmUnavailable = errors.New("c3mm metadata unavailable")

// getC3mm returns a C3MM part. If part 0 can't be fetched or parsed for a reason
// other than a network, auth or server error, it returns errC3mmUnavailable and
// remembers that, so tiles are probed directly without asking again.
func (ctx *context) getC3mm(p fly.Trigger, part int) (c3mm.C3MM, error) {
	ctx.mu.Lock()
	defer ctx.mu.Unlock()
	if ctx.c3mmUnavailable {
		return c3mm.C3MM{}, errC3mmUnavailable
	}
	if ctx.C3mms == nil || ctx.C3mms[part] == nil {
		data, err := ctx._getC3mm(p, part)
		if err != nil {
			if part == 0 && c3mmMissing(err) {
				ctx.c3mmUnavailable = true
				l.Println("C3MM metadata unavailable (", err, "); probing tiles directly")
				return c3mm.C3MM{}, errC3mmUnavailable
			}
			return c3mm.C3MM{}, err
		}
		if part == 0 {
			ctx.C3mms = make([]*c3mm.C3MM, len(data.FileIndex.Entries))
		}
		ctx.C3mms[part] = &data
		return data, nil
	}
	return *ctx.C3mms[part], nil
}

func (ctx *context) _getC3mm(p fly.Trigger, part int) (c3mm.C3MM, error) {
	fn := fmt.Sprintf("./cache/c3mm/%d_%d/%d", p.Region, p.Version, part)
	file, err := ioutil.ReadFile(fn)
	if err != nil && !os.IsNotExist(err) {
		return c3mm.C3MM{}, err
	}
	if err == nil {
		return parseC3mm(file, part)
	}
	data, err := ctx.get(fmt.Sprintf("%s?style=%d&v=%d&part=%d&region=%d",
		ctx.URLPrefixC3mm, mps.ResourceManifest_StyleConfig_C3MM_1, p.Version, part, p.Region))
	if err != nil {
		return c3mm.C3MM{}, err
	}
	if err = ioutil.WriteFile(fn+".tmp", data, 0655); err != nil {
		return c3mm.C3MM{}, err
	}
	if err = os.Rename(fn+".tmp", fn); err != nil {
		return c3mm.C3MM{}, err
	}
	return parseC3mm(data, part)
}

type c3mmParseError struct{ err error }

func (e c3mmParseError) Error() string { return "parse c3mm: " + e.err.Error() }
func (e c3mmParseError) Unwrap() error { return e.err }

func parseC3mm(data []byte, part int) (c3mm.C3MM, error) {
	m, err := c3mm.Parse(data, part)
	if err != nil {
		return c3mm.C3MM{}, c3mmParseError{err}
	}
	return m, nil
}

// c3mmMissing reports whether a C3MM error means the metadata isn't served
// (or is in an unknown format) rather than a transient or auth failure
func c3mmMissing(err error) bool {
	var pe c3mmParseError
	if isMissing(err) || errors.As(err, &pe) {
		return true
	}
	var se web.StatusError
	return errors.As(err, &se) && !web.IsAuthError(err) && se.Code < 500
}

func (ctx context) findPlace(lat, lon float64) (fly.Trigger, error) {
	// radius non spherical yet
	minDist, minPlace := math.Inf(1), fly.Trigger{}

	for _, v := range ctx.AltitudeManifest.Triggers {
		dist := math.Sqrt(math.Pow(lat-v.Lat, 2) + math.Pow(lon-v.Lon, 2))
		// radius can overlap. ignored for now
		if dist <= v.Radius && dist < minDist {
			minDist, minPlace = dist, v
		}
	}
	if minDist == math.Inf(1) {
		return fly.Trigger{}, errors.New("minPlace not found")
	}
	return minPlace, nil
}

func (ctx context) get(url string) ([]byte, error) {
	authURL, err := ctx.Context.AuthContext.AuthURL(url)
	if err != nil {
		return nil, err
	}
	return get(authURL)
}

// errNoData is returned when the server sends a jpeg instead of c3m(m) data,
// which it sometimes does when there is nothing at the requested location
var errNoData = errors.New("received jpeg instead of data")

// isMissing reports whether err means the requested resource doesn't exist
func isMissing(err error) bool {
	return web.IsNotFound(err) || errors.Is(err, errNoData)
}

func get(url string) (data []byte, err error) {
	data, err = web.GetWithCheck(url, func(res *http.Response) (err error) {
		if res.Header.Get("content-type") == "image/jpeg" {
			err = errNoData
		}
		return
	})
	return
}

type context struct {
	Context          mps.Context
	AltitudeManifest fly.AltitudeManifest
	URLPrefixC3mm    string
	URLPrefixC3m     string
	C3mms            []*c3mm.C3MM

	mu              *sync.Mutex
	c3mmUnavailable bool
}

func getContext(cache mps.Cache, config config.Config) (m context, err error) {
	ctx, err := mps.Init(cache, config)
	if err != nil {
		return
	}
	am, err := fly.GetAltitudeManifest(cache, ctx.ResourceManifest)
	if err != nil {
		return
	}

	c3mmURLPrefix, err := ctx.ResourceManifest.URLPrefixFromStyleID(mps.ResourceManifest_StyleConfig_C3MM_1)
	if err != nil {
		return
	}
	c3mURLPrefix, err := ctx.ResourceManifest.URLPrefixFromStyleID(mps.ResourceManifest_StyleConfig_C3M)
	if err != nil {
		return
	}

	m = context{Context: ctx, AltitudeManifest: am, URLPrefixC3mm: c3mmURLPrefix, URLPrefixC3m: c3mURLPrefix, mu: &sync.Mutex{}}
	return
}

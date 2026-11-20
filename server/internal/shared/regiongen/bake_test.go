package regiongen

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "rewrite testdata/regions/bakes.sha256 from the test plan and the shipped regions")

// reader takes little-endian values off a file, as the client will.
type reader struct {
	t *testing.T
	b []byte
}

func (r *reader) take(n int) []byte {
	r.t.Helper()
	if len(r.b) < n {
		r.t.Fatalf("the file ends %d bytes short", n-len(r.b))
	}
	v := r.b[:n]
	r.b = r.b[n:]
	return v
}

func (r *reader) u16() int { return int(binary.LittleEndian.Uint16(r.take(2))) }
func (r *reader) u32() int { return int(binary.LittleEndian.Uint32(r.take(4))) }
func (r *reader) f32() float64 {
	return float64(math.Float32frombits(binary.LittleEndian.Uint32(r.take(4))))
}

func (r *reader) header(magic string) [3]int {
	r.t.Helper()
	if m := string(r.take(4)); m != magic {
		r.t.Fatalf("magic %q; want %q", m, magic)
	}
	if v := r.u16(); v != Format {
		r.t.Fatalf("format %d; want %d", v, Format)
	}
	return [3]int{r.u16(), r.u16(), r.u16()}
}

func fileOf(t *testing.T, b Bake, path string) File {
	t.Helper()
	i := slices.IndexFunc(b.Files, func(f File) bool { return f.Path == path })
	if i < 0 {
		t.Fatalf("no %s in the bake", path)
	}
	return b.Files[i]
}

// Read back as the client will, every chunk holds its square's heights, colours and deep water, its edges shared with its neighbours, its heights with a border a sample wide held to the region, and the props and solids whose centres it holds, each once, each solid at its index in the region's list; the overview holds every 8th sample.
func TestTheBakeReadsBackAsTheRegion(t *testing.T) {
	r, b := smallRegion(t), smallBake(t)
	tr := r.Geometry.Terrain
	ncx, ncz := r.chunks()
	props, cyls, boxes := 0, 0, 0
	seenCyl, seenBox := map[int]bool{}, map[int]bool{}
	for cz := range ncz {
		for cx := range ncx {
			rd := &reader{t, fileOf(t, b, fmt.Sprintf("chunks/%d_%d.bin", cx, cz)).Data}
			if h := rd.header("FRCK"); h != [3]int{cx, cz, 65} {
				t.Fatalf("chunk %d_%d's header %v", cx, cz, h)
			}
			for iz := -1; iz <= 65; iz++ {
				for ix := -1; ix <= 65; ix++ {
					sx, sz := min(max(cx*64+ix, 0), tr.NX-1), min(max(cz*64+iz, 0), tr.NZ-1)
					if got, want := rd.f32(), tr.Heights[sz*tr.NX+sx]; got != want {
						t.Fatalf("chunk %d_%d's height at (%d, %d) is %v; want %v", cx, cz, ix, iz, got, want)
					}
				}
			}
			for iz := range 65 {
				for ix := range 65 {
					if got, want := rd.take(1)[0], r.Colours[(cz*64+iz)*tr.NX+cx*64+ix]; got != want {
						t.Fatalf("chunk %d_%d's colour at (%d, %d) is %d; want %d", cx, cz, ix, iz, got, want)
					}
				}
			}
			rd.take(3) // 65² colours, padded to 4 bytes
			for iz := range 65 {
				for ix := range 65 {
					if got, want := rd.take(1)[0] == 1, tr.DeepWater[(cz*64+iz)*tr.NX+cx*64+ix]; got != want {
						t.Fatalf("chunk %d_%d's deep water at (%d, %d) is %v; want %v", cx, cz, ix, iz, got, want)
					}
				}
			}
			rd.take(3)
			inChunk := func(x, z float64) {
				if gx, gz := r.chunkOf(x, z); gx != cx || gz != cz {
					t.Fatalf("(%v, %v) in chunk %d_%d; its centre is in %d_%d", x, z, cx, cz, gx, gz)
				}
			}
			for range rd.u32() {
				model, _ := rd.u16(), rd.u16()
				p := Prop{Model: model, X: rd.f32(), Z: rd.f32(), Yaw: rd.f32()}
				inChunk(p.X, p.Z)
				if !slices.Contains(r.Props, p) {
					t.Fatalf("chunk %d_%d holds prop %+v, which the region hasn't", cx, cz, p)
				}
				props++
			}
			for range rd.u32() {
				i, x, z, rad, top := rd.u32(), rd.f32(), rd.f32(), rd.f32(), rd.f32()
				inChunk(x, z)
				if c := r.Geometry.Cylinders[i]; c.X != x || c.Z != z || c.Radius != rad || c.Top != top || seenCyl[i] {
					t.Fatalf("chunk %d_%d holds a cylinder that isn't the region's %d, or is again", cx, cz, i)
				}
				seenCyl[i] = true
				cyls++
			}
			for range rd.u32() {
				i := rd.u32()
				var v [7]float64
				for k := range v {
					v[k] = rd.f32()
				}
				inChunk(v[0], v[1])
				if o := r.Geometry.Boxes[i]; [7]float64{o.X, o.Z, o.HalfW, o.HalfD, o.UX, o.UZ, o.Top} != v || seenBox[i] {
					t.Fatalf("chunk %d_%d holds a box that isn't the region's %d, or is again", cx, cz, i)
				}
				seenBox[i] = true
				boxes++
			}
			if len(rd.b) != 0 {
				t.Fatalf("chunk %d_%d has %d bytes left over", cx, cz, len(rd.b))
			}
		}
	}
	if props != len(r.Props) || cyls != len(r.Geometry.Cylinders) || boxes != len(r.Geometry.Boxes) {
		t.Errorf("chunks hold %d props, %d cylinders and %d boxes; the region %d, %d and %d", props, cyls, boxes, len(r.Props), len(r.Geometry.Cylinders), len(r.Geometry.Boxes))
	}

	rd := &reader{t, fileOf(t, b, "overview.bin").Data}
	if h := rd.header("FROV"); h != [3]int{33, 33, 8} {
		t.Fatalf("the overview's header %v; want 33 × 33 every 8 samples", h)
	}
	for iz := range 33 {
		for ix := range 33 {
			if got, want := rd.f32(), tr.Heights[iz*8*tr.NX+ix*8]; got != want {
				t.Fatalf("the overview's height at (%d, %d) is %v; want %v", ix, iz, got, want)
			}
		}
	}
	rd.take(33 * 33)
	if len(rd.b) != 0 {
		t.Fatalf("the overview has %d bytes left over", len(rd.b))
	}
}

// region.json names the palette and models the chunks index, counts the solids they index into, and carries the plan's places and the water.
func TestRegionJSONIndexesTheChunks(t *testing.T) {
	r, b := smallRegion(t), smallBake(t)
	var m struct {
		Format  int
		Chunks  [2]int
		Heights [2]float64
		Solids  struct{ Cylinders, Boxes int }
		Palette []struct{ Name, RGB string }
		Models  []string
		Rivers  []struct{ Levels []float64 }
		Areas   []struct{ ID string }
		Start   struct{ At [2]float64 }
	}
	if err := json.Unmarshal(fileOf(t, b, "region.json").Data, &m); err != nil {
		t.Fatal(err)
	}
	k := shippedKit(t)
	if m.Solids.Cylinders != len(r.Geometry.Cylinders) || m.Solids.Boxes != len(r.Geometry.Boxes) || m.Solids.Cylinders == 0 || m.Solids.Boxes == 0 {
		t.Errorf("solids %+v; want the region's %d cylinders and %d boxes", m.Solids, len(r.Geometry.Cylinders), len(r.Geometry.Boxes))
	}
	if m.Format != Format || m.Chunks != [2]int{4, 4} || m.Heights != r.Heights || !slices.Equal(m.Models, r.Models) || len(m.Areas) != 5 || m.Start.At != [2]float64{124, 80} {
		t.Errorf("region.json %+v", m)
	}
	for i, c := range m.Palette {
		if c.Name != r.Palette[i] || c.RGB != k.Grounds[c.Name] {
			t.Errorf("palette %d is %s %s; want %s %s", i, c.Name, c.RGB, r.Palette[i], k.Grounds[r.Palette[i]])
		}
	}
	if len(m.Rivers) != 1 || !slices.Equal(m.Rivers[0].Levels, r.Rivers[0].Levels) {
		t.Errorf("rivers %+v; want the region's levels", m.Rivers)
	}
}

// The map tiles are the region's image cut in 128 m squares from the south-west, and a quarter of it averaged 4 × 4.
func TestTilesCutTheMapImage(t *testing.T) {
	r, b := smallRegion(t), smallBake(t)
	img := r.Image()
	fine := fileOf(t, b, "tiles/1/1_0.png").Tile // east of the south-west tile
	if fine.Bounds().Dx() != 128 || fine.NRGBAAt(5, 7) != img.NRGBAAt(128+5, 128+7) {
		t.Errorf("tile 1_0's pixel (5, 7) is %v; want the image's (133, 135), %v", fine.NRGBAAt(5, 7), img.NRGBAAt(133, 135))
	}
	coarse := fileOf(t, b, "tiles/4/1_0.png").Tile
	sum := 0
	for dy := range 4 {
		for dx := range 4 {
			sum += int(fine.NRGBAAt(8+dx, 12+dy).G)
		}
	}
	if coarse.Bounds().Dx() != 32 || int(coarse.NRGBAAt(2, 3).G) != sum/16 {
		t.Errorf("tile 4/1_0's pixel (2, 3) green %d; want the average %d", coarse.NRGBAAt(2, 3).G, sum/16)
	}
	n := 0
	for _, f := range b.Files {
		if strings.HasPrefix(f.Path, "tiles/") {
			n++
		}
	}
	if n != 8 {
		t.Errorf("%d tiles; want 4 squares at each of 2 scales", n)
	}
}

// Each plan's bake hash, the test plans' and every shipped region's: a change to the generator shows in review. The same hashes hold on amd64 and arm64.
func TestBakeHashes(t *testing.T) {
	const golden = "../../../testdata/regions/bakes.sha256"
	plans, err := filepath.Glob("../../../data/regions/*.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	hash := func(id string, p Plan) {
		b, err := build(t, p).Bake()
		if err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(&out, "%s %s\n", id, b.Hash)
	}
	hash("small", small(t))
	hash("wide", plan(t, "testdata/wide.yaml"))
	for _, path := range plans {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		p, err := Decode(raw)
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		hash(strings.TrimSuffix(filepath.Base(path), ".yaml"), p)
	}
	if *update {
		if err := os.MkdirAll(filepath.Dir(golden), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(golden, out.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	if out.String() != string(want) {
		t.Errorf("bakes differ (go test ./internal/shared/regiongen -update after a change to the generator or a plan)\ngot:\n%swant:\n%s", out.String(), want)
	}
}

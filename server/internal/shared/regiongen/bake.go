package regiongen

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"math"
	"slices"
	"strings"

	"finga/internal/shared/sim"
)

// Format is the bake's version, in every file; a reader refuses another. 2: a chunk's heights carry a border. 3: a chunk carries its deep water and each solid's index in the region's lists.
const Format = 3

// tileM is a map tile's side; tiles are drawn at 1 and 4 metres a pixel.
const tileM = 128

// File is one file of a bake: its bytes, or a map tile's pixels.
type File struct {
	Path string
	Data []byte
	Tile *image.NRGBA
}

// Bake is a region's files as the client reads them (large-regions.md section 3), and their hash, which names the folder they're served from.
type Bake struct {
	Files []File // by path
	Hash  string // hex SHA-256 of each file's path and bytes, a tile's by its pixels
}

// Folder is the first 16 hex digits of the bake's hash, naming the folder it's served from: the server names the one it built, so a client never reads a bake older than its region.
func (b Bake) Folder() string { return b.Hash[:16] }

// Bake is r's files.
func (r *Region) Bake() (Bake, error) {
	meta, err := r.meta()
	if err != nil {
		return Bake{}, err
	}
	files := []File{{Path: "region.json", Data: meta}, {Path: "overview.bin", Data: r.overview()}}
	ncx, ncz := r.chunks()
	for cz := range ncz {
		for cx := range ncx {
			files = append(files, File{Path: fmt.Sprintf("chunks/%d_%d.bin", cx, cz), Data: r.chunk(cx, cz)})
		}
	}
	files = append(files, r.tiles()...)
	if len(r.Flames) > 0 { // the server bakes it too, as the folder's name hashes every file
		files = append(files, File{Path: "light.bin", Data: r.light()})
	}
	slices.SortFunc(files, func(a, b File) int { return strings.Compare(a.Path, b.Path) })
	h := sha256.New()
	for _, f := range files {
		h.Write([]byte(f.Path))
		h.Write([]byte{0})
		if f.Tile != nil {
			b := f.Tile.Bounds()
			h.Write(binary.LittleEndian.AppendUint32(binary.LittleEndian.AppendUint32(nil, uint32(b.Dx())), uint32(b.Dy()))) //nolint:gosec // a tile is 128 px
			h.Write(f.Tile.Pix)
			continue
		}
		h.Write(f.Data)
	}
	return Bake{Files: files, Hash: hex.EncodeToString(h.Sum(nil))}, nil
}

func (r *Region) chunks() (int, int) {
	return int(r.Plan.Size[0] / chunkM), int(r.Plan.Size[1] / chunkM)
}

// chunkOf is the chunk holding (x, z), the far sides' in the last chunks.
func (r *Region) chunkOf(x, z float64) (int, int) {
	ncx, ncz := r.chunks()
	return min(max(int(math.Floor(x/chunkM)), 0), ncx-1), min(max(int(math.Floor(z/chunkM)), 0), ncz-1)
}

// writer appends little-endian values.
type writer struct{ b []byte }

func (w *writer) u16(v int) { w.b = binary.LittleEndian.AppendUint16(w.b, uint16(v)) } //nolint:gosec // counts and indices the format caps
func (w *writer) u32(v int) { w.b = binary.LittleEndian.AppendUint32(w.b, uint32(v)) } //nolint:gosec // as above
func (w *writer) f32(v float64) {
	w.b = binary.LittleEndian.AppendUint32(w.b, math.Float32bits(float32(v)))
}

// pad aligns the next value to 4 bytes, so a reader can view a run of float32s in place.
func (w *writer) pad() {
	for len(w.b)%4 != 0 {
		w.b = append(w.b, 0)
	}
}

// header is a file's magic, the format, and three fields of its own.
func (w *writer) header(magic string, a, b, c int) {
	w.b = append(w.b, magic...)
	w.u16(Format)
	w.u16(a)
	w.u16(b)
	w.u16(c)
}

// chunk is one 64 m square: "FRCK", the format, its x and z, and its samples a side n (its edges shared with its neighbours); then (n+2)² heights, its own with a border one sample wide, held to the region at its edges, so a client takes an edge's normal from the same numbers on both sides; then n² colours and n² barrier marks, 1 or 0, row by row from the south-west; and the props, cylinders and boxes whose centres it holds, each solid with its index in the region's list, so a client visits them in the server's order.
func (r *Region) chunk(cx, cz int) []byte {
	t := r.Geometry.Terrain
	per := int(chunkM / t.Cell)
	var w writer
	w.header("FRCK", cx, cz, per+1)
	for iz := -1; iz <= per+1; iz++ {
		for ix := -1; ix <= per+1; ix++ {
			sx, sz := min(max(cx*per+ix, 0), t.NX-1), min(max(cz*per+iz, 0), t.NZ-1)
			w.f32(t.Heights[sz*t.NX+sx])
		}
	}
	for iz := range per + 1 {
		for ix := range per + 1 {
			w.b = append(w.b, r.Colours[(cz*per+iz)*t.NX+cx*per+ix])
		}
	}
	w.pad()
	for iz := range per + 1 {
		for ix := range per + 1 {
			w.b = append(w.b, 0)
			if t.Barrier[(cz*per+iz)*t.NX+cx*per+ix] {
				w.b[len(w.b)-1] = 1
			}
		}
	}
	w.pad()
	in := func(x, z float64) bool {
		x0, z0 := r.chunkOf(x, z)
		return x0 == cx && z0 == cz
	}
	var props []Prop
	for _, p := range r.Props {
		if in(p.X, p.Z) {
			props = append(props, p)
		}
	}
	w.u32(len(props))
	for _, p := range props {
		w.u16(p.Model)
		w.u16(0)
		w.f32(p.X)
		w.f32(p.Z)
		w.f32(p.Yaw)
	}
	var cyls []int
	for i, c := range r.Geometry.Cylinders {
		if in(c.X, c.Z) {
			cyls = append(cyls, i)
		}
	}
	w.u32(len(cyls))
	for _, i := range cyls {
		c := r.Geometry.Cylinders[i]
		w.u32(i)
		for _, v := range [...]float64{c.X, c.Z, c.Radius, c.Top} {
			w.f32(v)
		}
	}
	var boxes []int
	for i, b := range r.Geometry.Boxes {
		if in(b.X, b.Z) {
			boxes = append(boxes, i)
		}
	}
	w.u32(len(boxes))
	for _, i := range boxes {
		b := r.Geometry.Boxes[i]
		w.u32(i)
		for _, v := range [...]float64{b.X, b.Z, b.HalfW, b.HalfD, b.UX, b.UZ, b.Top} {
			w.f32(v)
		}
	}
	return w.b
}

// overview is the whole region every 8 m: "FROV", the format, its samples along x and z, and its spacing in samples; then heights and colours row by row from the south-west corner.
func (r *Region) overview() []byte {
	t := r.Geometry.Terrain
	step := int(overviewM / t.Cell)
	nx, nz := (t.NX-1)/step+1, (t.NZ-1)/step+1
	var w writer
	w.header("FROV", nx, nz, step)
	for iz := range nz {
		for ix := range nx {
			w.f32(t.Heights[iz*step*t.NX+ix*step])
		}
	}
	for iz := range nz {
		for ix := range nx {
			w.b = append(w.b, r.Colours[iz*step*t.NX+ix*step])
		}
	}
	return w.b
}

// meta is region.json: what the client needs besides the ground to draw the region and its map.
func (r *Region) meta() ([]byte, error) {
	p := r.Plan
	type colour struct {
		Name string `json:"name"`
		RGB  string `json:"rgb"`
	}
	type river struct {
		Path   [][2]float64 `json:"path"`
		Width  float64      `json:"width"`
		Levels []float64    `json:"levels"`
	}
	type lake struct {
		Area  [][2]float64 `json:"area"`
		Level float64      `json:"level"`
	}
	type solids struct {
		Cylinders int     `json:"cylinders"`
		Boxes     int     `json:"boxes"`
		Reach     float64 `json:"reach"` // the farthest any solid reaches from its centre, so from the chunk that carries it
	}
	m := struct {
		Format    int        `json:"format"`
		Name      string     `json:"name"`
		Under     string     `json:"under"` // a place below's region above; empty on the land
		Sky       string     `json:"sky"`
		Size      [2]float64 `json:"size"`
		Cell      float64    `json:"cell"`
		Chunk     float64    `json:"chunk"`
		Chunks    [2]int     `json:"chunks"`
		Overview  float64    `json:"overview"`
		Heights   [2]float64 `json:"heights"`
		Solids    solids     `json:"solids"` // how many of each the chunks index into
		Palette   []colour   `json:"palette"`
		Models    []string   `json:"models"`
		Rivers    []river    `json:"rivers"`
		Lakes     []lake     `json:"lakes"`
		Areas     []Area     `json:"areas"`
		Camps     []Camp     `json:"camps"`
		Waypoints []Waypoint `json:"waypoints"`
		NPCs      []NPC      `json:"npcs"`
		Gates     []Gate     `json:"gates"`
		Start     Place      `json:"start"`
		Lights    []Flame    `json:"lights"` // each torch's flame, for its flicker; light.bin holds their light
	}{
		Format: Format, Name: p.Name, Under: p.Under, Sky: p.Look.Sky, Lights: nonNil(r.Flames), Size: p.Size, Cell: p.Cell, Chunk: chunkM, Overview: overviewM, Heights: r.Heights,
		Solids: solids{len(r.Geometry.Cylinders), len(r.Geometry.Boxes), solidReach(r.Geometry)},
		Models: r.Models, Areas: nonNil(p.Areas), Camps: nonNil(p.Camps), Waypoints: nonNil(p.Waypoints), NPCs: nonNil(p.NPCs), Gates: nonNil(p.Gates), Start: p.Start,
		Rivers: []river{}, Lakes: []lake{},
	}
	m.Chunks[0], m.Chunks[1] = r.chunks()
	for i, name := range r.Palette {
		m.Palette = append(m.Palette, colour{name, fmt.Sprintf("#%06x", r.RGB[i])})
	}
	for _, rv := range r.Rivers {
		m.Rivers = append(m.Rivers, river(rv))
	}
	for _, l := range r.Lakes {
		m.Lakes = append(m.Lakes, lake(l))
	}
	return json.Marshal(m)
}

func nonNil[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

// tiles are the map image cut in 128 m squares, at 1 m a pixel under tiles/1/ and 4 m under tiles/4/, each named <x>_<z> counting from the south-west.
func (r *Region) tiles() []File {
	img := r.Image()
	ncx, ncz := int(r.Plan.Size[0]/tileM), int(r.Plan.Size[1]/tileM)
	h := img.Bounds().Dy()
	var files []File
	for tz := range ncz {
		for tx := range ncx {
			x0, y0 := tx*tileM, h-(tz+1)*tileM
			fine := image.NewNRGBA(image.Rect(0, 0, tileM, tileM))
			coarse := image.NewNRGBA(image.Rect(0, 0, tileM/4, tileM/4))
			for y := range tileM {
				copy(fine.Pix[y*fine.Stride:(y+1)*fine.Stride], img.Pix[(y0+y)*img.Stride+x0*4:(y0+y)*img.Stride+(x0+tileM)*4])
			}
			for y := range tileM / 4 {
				for x := range tileM / 4 {
					var sum [3]int
					for dy := range 4 {
						for dx := range 4 {
							c := fine.NRGBAAt(4*x+dx, 4*y+dy)
							sum[0], sum[1], sum[2] = sum[0]+int(c.R), sum[1]+int(c.G), sum[2]+int(c.B)
						}
					}
					coarse.SetNRGBA(x, y, color.NRGBA{R: uint8(sum[0] / 16), G: uint8(sum[1] / 16), B: uint8(sum[2] / 16), A: 255}) //nolint:gosec // an average of bytes
				}
			}
			files = append(files, File{Path: fmt.Sprintf("tiles/1/%d_%d.png", tx, tz), Tile: fine}, File{Path: fmt.Sprintf("tiles/4/%d_%d.png", tx, tz), Tile: coarse})
		}
	}
	return files
}

// Image is the region from above at 1 m a pixel, north up: each ground's colour shaded by its slope to the light from the north-west, water over beds, solids dark.
func (r *Region) Image() *image.NRGBA {
	t := r.Geometry.Terrain
	w, h := int(r.Plan.Size[0]), int(r.Plan.Size[1])
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	water := r.RGB[slices.Index(r.Palette, waterColour)]
	g := grid{nx: t.NX, nz: t.NZ, cell: t.Cell}
	for py := range h {
		z := float64(h-py) - 0.5
		for px := range w {
			x := float64(px) + 0.5
			s := g.nearest(x, z)
			ix, iz := s%t.NX, s/t.NX
			rgb, shade := r.RGB[r.Colours[s]], 1.0
			if r.Wet[s] {
				rgb = water
			} else {
				nw := t.Heights[g.index(g.clampX(ix-1), g.clampZ(iz+1))]
				se := t.Heights[g.index(g.clampX(ix+1), g.clampZ(iz-1))]
				shade = min(max(1+float64(0.15*((nw-se)/t.Cell)), 0.6), 1.4)
			}
			img.SetNRGBA(px, py, shaded(rgb, shade))
		}
	}
	r.darkenSolids(img)
	return img
}

// shaded is rgb with each channel times shade, held to a byte.
func shaded(rgb uint32, shade float64) color.NRGBA {
	ch := func(v uint32) uint8 { return uint8(min(math.Floor(float64(float64(v&0xff)*shade)), 255)) }
	return color.NRGBA{R: ch(rgb >> 16), G: ch(rgb >> 8), B: ch(rgb), A: 255}
}

// darkenSolids dims each pixel whose middle a solid covers.
func (r *Region) darkenSolids(img *image.NRGBA) {
	h := img.Bounds().Dy()
	dim := func(lo, hi [2]float64, covers func(x, z float64) bool) {
		for py := max(0, h-int(math.Ceil(hi[1]))); py < min(h, h-int(math.Floor(lo[1]))+1); py++ {
			for px := max(0, int(math.Floor(lo[0]))); px < min(img.Bounds().Dx(), int(math.Ceil(hi[0]))+1); px++ {
				if x, z := float64(px)+0.5, float64(h-py)-0.5; covers(x, z) {
					c := img.NRGBAAt(px, py)
					img.SetNRGBA(px, py, color.NRGBA{R: c.R / 2, G: c.G / 2, B: c.B / 2, A: 255})
				}
			}
		}
	}
	for _, c := range r.Geometry.Cylinders {
		dim([2]float64{c.X - c.Radius, c.Z - c.Radius}, [2]float64{c.X + c.Radius, c.Z + c.Radius}, func(x, z float64) bool {
			dx, dz := x-c.X, z-c.Z
			return float64(dx*dx)+float64(dz*dz) <= float64(c.Radius*c.Radius)
		})
	}
	for _, b := range r.Geometry.Boxes {
		reach := math.Sqrt(float64(b.HalfW*b.HalfW) + float64(b.HalfD*b.HalfD))
		dim([2]float64{b.X - reach, b.Z - reach}, [2]float64{b.X + reach, b.Z + reach}, func(x, z float64) bool {
			dx, dz := x-b.X, z-b.Z
			u, v := float64(dx*b.UX)+float64(dz*b.UZ), float64(dz*b.UX)-float64(dx*b.UZ)
			return math.Abs(u) <= b.HalfW && math.Abs(v) <= b.HalfD
		})
	}
}

// solidReach is the farthest any of g's solids reaches from its centre: a cylinder's radius, a box's half diagonal.
func solidReach(g sim.Geometry) float64 {
	reach := 0.0
	for _, c := range g.Cylinders {
		reach = max(reach, c.Radius)
	}
	for _, b := range g.Boxes {
		reach = max(reach, math.Sqrt(float64(b.HalfW*b.HalfW)+float64(b.HalfD*b.HalfD))) // no fused multiply-add, so every CPU agrees
	}
	return reach
}

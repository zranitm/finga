// Command tmj2yaml turns 2D Tiled maps into flat 3D placeholders (docs/tech/3d-world.md section 4): go run ./cmd/tmj2yaml cave …
package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"log"
	"math"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"

	"finga/internal/maps3d"
	"finga/internal/nav"
	"finga/pkg/tiled"
)

const (
	pxPerMetre   = 32
	depth        = 12 // the corridor's z extent
	middle       = depth / 2
	npcLine      = 8.5  // NPCs stand north of the middle, off the spawns' line
	portalRadius = 1.2  // a portal's circle
	portalInset  = 2    // a portal's centre from its end
	maxCircle    = 5.0  // a spawn's or boss's circle keeps a metre inside the corridor
	offPortal    = 0.25 // and keeps this far off any portal
	marker       = "# Converted from %s.tmj by cmd/tmj2yaml: a flat placeholder until its own design.\n"
)

func main() {
	from := flag.String("from", "data/maps", "the folder of .tmj maps")
	to := flag.String("to", "data/maps3d", "the folder the .yaml maps are written to")
	flag.Parse()
	for _, id := range flag.Args() {
		out, err := convertFile(filepath.Join(*from, id+".tmj"), id)
		if err != nil {
			log.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(*to, id+".yaml"), out, 0o644); err != nil {
			log.Fatal(err)
		}
	}
}

// convertFile is the placeholder YAML for the .tmj at path, with the marker line that tells it from a designed map.
func convertFile(path, id string) ([]byte, error) {
	tm, err := tiled.Load(path)
	if err != nil {
		return nil, err
	}
	f, err := convert(tm)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return encode(f, id)
}

func convert(tm *tiled.Map) (maps3d.File, error) {
	kind, _ := tm.Properties.String("kind")
	name, _ := tm.Properties.String("name")
	ground, _ := tm.Properties.String("ground")
	sky, _ := tm.Properties.String("background")
	length := tm.PixelWidth() / pxPerMetre
	f := maps3d.File{Name: name, Kind: kind, Look: maps3d.Look{Ground: ground, Sky: sky}, Size: [2]float64{length, depth}, Terrain: maps3d.Terrain{Cell: cell(length)}}
	f.Start, _ = tm.Properties.Bool("start")
	if kind == "hunting" {
		f.Town, _ = tm.Properties.String("town")
		lo, _ := tm.Properties.Int("min_level")
		hi, _ := tm.Properties.Int("max_level")
		f.Levels = [2]int{lo, hi}
	}
	layers := tm.ObjectGroups()
	atEnd := map[float64]int{} // portals already at each end, which the next one stands beside
	for _, o := range layers["portals"].Objects {
		target, _ := o.Properties.String("target_map")
		portal, _ := o.Properties.String("target_portal")
		x := float64(portalInset)
		if along(o)[0] > length/2 {
			x = length - portalInset
		}
		at := [2]float64{x, middle + [3]float64{0, -3, 3}[atEnd[x]]}
		atEnd[x]++
		f.Portals = append(f.Portals, maps3d.Portal{Name: o.Name, At: at, Radius: portalRadius, To: maps3d.Target{Map: target, Portal: portal}})
	}
	for _, o := range layers["spawns"].Objects {
		mob, _ := o.Properties.String("mob")
		maxMobs, _ := o.Properties.Int("max")
		respawn, _ := o.Properties.Int("respawn_seconds")
		r, err := circle(f.Portals, o)
		if err != nil {
			return f, fmt.Errorf("spawn %q: %w", o.Name, err)
		}
		f.Spawns = append(f.Spawns, maps3d.Spawn{Name: o.Name, At: along(o), Radius: r, Mob: mob, Max: maxMobs, Respawn: time.Duration(respawn) * time.Second})
	}
	if spawns := layers["player_spawn"].Objects; len(spawns) == 1 {
		f.PlayerSpawn = maps3d.Place{At: along(spawns[0]), Yaw: 90}
		if f.PlayerSpawn.At[0] > length/2 {
			f.PlayerSpawn.Yaw = 270
		}
	}
	for _, o := range layers["npcs"].Objects {
		f.NPCs = append(f.NPCs, maps3d.NPC{ID: o.Name, At: [2]float64{(o.X + o.Width/2) / pxPerMetre, npcLine}, Yaw: 180})
	}
	if bosses := layers["boss"].Objects; len(bosses) == 1 {
		o := bosses[0]
		mob, _ := o.Properties.String("mob")
		kills, _ := o.Properties.Int("kills")
		r, err := circle(f.Portals, o)
		if err != nil {
			return f, fmt.Errorf("boss %q: %w", o.Name, err)
		}
		f.Boss = &maps3d.Boss{At: along(o), Radius: r, Mob: mob, Kills: kills}
	}
	return f, nil
}

// cell is the widest whole metre dividing the corridor's length and depth: flat ground needs no finer height samples.
func cell(length float64) float64 {
	for c := float64(depth); c >= 1; c-- {
		if math.Mod(length, c) == 0 && math.Mod(depth, c) == 0 {
			return c
		}
	}
	return 0.5
}

// along is an object's spot on the corridor's middle line: its centre along the 2D map's length.
func along(o tiled.Object) [2]float64 {
	return [2]float64{(o.X + o.Width/2) / pxPerMetre, middle}
}

// circle is the radius of an area's circle: half its 2D width, kept inside the corridor and off every portal.
func circle(portals []maps3d.Portal, o tiled.Object) (float64, error) {
	at := along(o)
	r := min(o.Width/pxPerMetre/2, maxCircle)
	for _, p := range portals {
		r = min(r, nav.Distance(maps3d.Point(at), maps3d.Point(p.At))-p.Radius-offPortal)
	}
	if r <= 0 {
		return 0, errors.New("sits on a portal")
	}
	return r, nil
}

// encode writes f with its list items each on one line, as the designed maps are written.
func encode(f maps3d.File, id string) ([]byte, error) {
	var doc yaml.Node
	if err := doc.Encode(f); err != nil {
		return nil, err
	}
	for i := 0; i+1 < len(doc.Content); i += 2 {
		if v := doc.Content[i+1]; v.Kind == yaml.SequenceNode {
			for _, item := range v.Content {
				item.Style = yaml.FlowStyle
			}
		}
	}
	var out bytes.Buffer
	fmt.Fprintf(&out, marker, id)
	enc := yaml.NewEncoder(&out)
	enc.SetIndent(2)
	if err := enc.Encode(&doc); err != nil {
		return nil, err
	}
	return out.Bytes(), enc.Close()
}

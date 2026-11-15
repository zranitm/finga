// Command mapjson writes each map's geometry as Go builds it, with its art and without its boss, for the client (3d-world.md section 4).
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"finga/internal/gamedata/load"
	"finga/internal/mapfile"
	"finga/internal/sim"
)

// Map is one map as the client reads it.
type Map struct {
	ID             string       `json:"id"`
	Ground         string       `json:"ground"` // art keys
	Sky            string       `json:"sky"`
	Geometry       sim.Geometry `json:"geometry"`
	BoxModels      []Model      `json:"box_models"` // one per geometry box, in order
	CylinderModels []string     `json:"cylinder_models"`
	Props          []Prop       `json:"props"`
	Portals        []Portal     `json:"portals"`
}

// Model is a box's model and its turn from facing north toward east, in degrees.
type Model struct {
	Model string  `json:"model"`
	Yaw   float64 `json:"yaw"`
}

// Prop is looks only: one model at a spot, or a scatter the client expands from its seed.
type Prop struct {
	At      *[2]float64 `json:"at,omitempty"`
	Yaw     float64     `json:"yaw,omitempty"`
	Model   string      `json:"model,omitempty"`
	Scatter *Scatter    `json:"scatter,omitempty"`
}

// Scatter is Count of one model strewn over Area, [x, z, width, depth] from its south-west corner.
type Scatter struct {
	Model string     `json:"model"`
	Area  [4]float64 `json:"area"`
	Count int        `json:"count"`
	Seed  int64      `json:"seed"`
}

// Portal is a portal's circle and the map it leads to.
type Portal struct {
	Name   string     `json:"name"`
	At     [2]float64 `json:"at"`
	Radius float64    `json:"radius"`
	To     string     `json:"to"`
}

func main() {
	data := flag.String("data", "data", "the data folder")
	out := flag.String("out", "", "the folder to write each map's <id>.json in")
	flag.Parse()
	if err := run(*data, *out); err != nil {
		fmt.Fprintln(os.Stderr, "mapjson:", err)
		os.Exit(1)
	}
}

// run checks the data as the server loads it, then writes every map to out, which it empties first.
func run(dataDir, out string) error {
	if out == "" {
		return fmt.Errorf("-out is required")
	}
	d, err := load.Dir(dataDir)
	if err != nil {
		return err
	}
	stale, err := filepath.Glob(filepath.Join(out, "*.json"))
	if err != nil {
		return err
	}
	for _, f := range stale { // only its own files: -out may name a folder with others in it
		if err := os.Remove(f); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(out, 0o755); err != nil {
		return err
	}
	for _, m := range d.Maps() {
		cm, err := build(dataDir, m.ID, d.Movement())
		if err != nil {
			return err
		}
		raw, err := json.Marshal(cm)
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(out, m.ID+".json"), raw, 0o644); err != nil {
			return err
		}
	}
	fmt.Printf("wrote %d maps to %s\n", len(d.Maps()), out)
	return nil
}

func build(dataDir, id string, p sim.Params) (Map, error) {
	raw, err := os.ReadFile(filepath.Join(dataDir, "maps", id+".yaml"))
	if err != nil {
		return Map{}, err
	}
	var f mapfile.File
	dec := yaml.NewDecoder(strings.NewReader(string(raw)))
	dec.KnownFields(true)
	if err := dec.Decode(&f); err != nil {
		return Map{}, fmt.Errorf("%s: %w", id, err)
	}
	built, err := mapfile.Build(id, f, p)
	if err != nil {
		return Map{}, fmt.Errorf("%s: %w", id, err)
	}
	m := Map{ID: id, Ground: f.Look.Ground, Sky: f.Look.Sky, Geometry: built.Geometry, BoxModels: []Model{}, CylinderModels: []string{}, Props: []Prop{}, Portals: []Portal{}}
	for _, s := range f.Solids { // Build adds each in file order, to the boxes or the cylinders
		if s.Box != nil {
			m.BoxModels = append(m.BoxModels, Model{Model: s.Model, Yaw: s.Yaw})
		} else {
			m.CylinderModels = append(m.CylinderModels, s.Model)
		}
	}
	for _, pr := range f.Props {
		out := Prop{At: pr.At, Yaw: pr.Yaw, Model: pr.Model}
		if sc := pr.Scatter; sc != nil {
			out.Scatter = &Scatter{Model: sc.Model, Area: sc.Area, Count: sc.Count, Seed: sc.Seed}
		}
		m.Props = append(m.Props, out)
	}
	for _, pt := range f.Portals {
		m.Portals = append(m.Portals, Portal{Name: pt.Name, At: pt.At, Radius: pt.Radius, To: pt.To.Map})
	}
	return m, nil
}

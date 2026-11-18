package regiongen

import (
	"bytes"
	"errors"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strconv"

	"gopkg.in/yaml.v3"
)

// Kit is data/kit.yaml: the ground colours a region is painted with and the client draws, and each model's solid.
type Kit struct {
	Grounds map[string]string `yaml:"grounds"` // name → sRGB, "#rrggbb"
	Models  map[string]Solid  `yaml:"models"`
}

// Solid is a model's footprint: a cylinder, a box, or none for looks only.
type Solid struct {
	Cylinder *CylinderSolid `yaml:"cylinder,omitempty,flow"`
	Box      *BoxSolid      `yaml:"box,omitempty,flow"`
}

// CylinderSolid stands Height above the ground at its centre.
type CylinderSolid struct {
	Radius float64 `yaml:"radius"`
	Height float64 `yaml:"height"`
}

// BoxSolid is Width across its yaw and Depth along it, Height above the ground at its centre.
type BoxSolid struct {
	Width  float64 `yaml:"width"`
	Depth  float64 `yaml:"depth"`
	Height float64 `yaml:"height"`
}

var rgb = regexp.MustCompile(`^#[0-9a-f]{6}$`)

// DecodeKit reads a kit strictly and checks it.
func DecodeKit(raw []byte) (Kit, error) {
	var k Kit
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	dec.KnownFields(true)
	if err := dec.Decode(&k); err != nil {
		return Kit{}, err
	}
	if len(k.Grounds) == 0 || len(k.Grounds) > 256 {
		return Kit{}, errors.New("kit needs 1 to 256 grounds")
	}
	for _, name := range slices.Sorted(maps.Keys(k.Grounds)) {
		if !rgb.MatchString(k.Grounds[name]) {
			return Kit{}, fmt.Errorf("ground %s: %q isn't #rrggbb in lower case", name, k.Grounds[name])
		}
	}
	if len(k.Models) > 1<<16 {
		return Kit{}, errors.New("kit has more models than a chunk can name")
	}
	for _, name := range slices.Sorted(maps.Keys(k.Models)) {
		s := k.Models[name]
		switch {
		case s.Cylinder != nil && s.Box != nil:
			return Kit{}, fmt.Errorf("model %s: a cylinder or a box, not both", name)
		case s.Cylinder != nil && (!reach(s.Cylinder.Radius, maxReach) || !reach(s.Cylinder.Height, maxHeight)):
			return Kit{}, fmt.Errorf("model %s: a cylinder needs a radius up to %d m and a height up to %d m", name, maxReach, maxHeight)
		case s.Box != nil && (!reach(s.Box.Width, maxReach) || !reach(s.Box.Depth, maxReach) || !reach(s.Box.Height, maxHeight)):
			return Kit{}, fmt.Errorf("model %s: a box needs a width and depth up to %d m and a height up to %d m", name, maxReach, maxHeight)
		}
	}
	return k, nil
}

// palette is the kit's ground names in order, each a colour index of the bake.
func (k Kit) palette() []string { return slices.Sorted(maps.Keys(k.Grounds)) }

// rgbOf is a ground's colour as 0xrrggbb.
func (k Kit) rgbOf(name string) uint32 {
	v, _ := strconv.ParseUint(k.Grounds[name][1:], 16, 32)
	return uint32(v)
}

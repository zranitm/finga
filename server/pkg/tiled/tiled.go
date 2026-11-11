// Package tiled reads Tiled JSON maps (.tmj), checking the format only.
package tiled

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// ObjectGroup is the type of a layer that holds objects.
const ObjectGroup = "objectgroup"

// Map is the subset of Tiled's map format this package reads.
type Map struct {
	Type       string     `json:"type"`
	Width      int        `json:"width"` // tiles
	Height     int        `json:"height"`
	TileWidth  int        `json:"tilewidth"` // pixels
	TileHeight int        `json:"tileheight"`
	Properties Properties `json:"properties"`
	Layers     []Layer    `json:"layers"`
}

// Layer is a tile layer or an object group, in file order.
type Layer struct {
	Name    string   `json:"name"`
	Type    string   `json:"type"`
	Objects []Object `json:"objects"`
}

// Object is a shape on an object group.
type Object struct {
	ID         int        `json:"id"`
	Name       string     `json:"name"`
	X          float64    `json:"x"`
	Y          float64    `json:"y"`
	Width      float64    `json:"width"`
	Height     float64    `json:"height"`
	Properties Properties `json:"properties"`
}

// Property is a custom property; Value is a JSON-decoded string, float64 or bool.
type Property struct {
	Name  string `json:"name"`
	Type  string `json:"type"`
	Value any    `json:"value"`
}

// Properties is a list of custom properties.
type Properties []Property

// String is the named property when it is a string.
func (ps Properties) String(name string) (string, bool) {
	for _, p := range ps {
		if p.Name == name {
			s, ok := p.Value.(string)
			return s, ok
		}
	}
	return "", false
}

// Int is the named property when it is a whole number.
func (ps Properties) Int(name string) (int, bool) {
	for _, p := range ps {
		if p.Name == name {
			f, ok := p.Value.(float64) // JSON numbers
			if !ok || f != float64(int(f)) {
				return 0, false
			}
			return int(f), true
		}
	}
	return 0, false
}

// Bool is the named property when it is a boolean.
func (ps Properties) Bool(name string) (bool, bool) {
	for _, p := range ps {
		if p.Name == name {
			b, ok := p.Value.(bool)
			return b, ok
		}
	}
	return false, false
}

var errNotMap = errors.New("not a Tiled map with a tile size")

// Parse reads one map.
func Parse(data []byte) (*Map, error) {
	var m Map
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parse: %w", err)
	}
	if m.Type != "map" || m.TileWidth <= 0 || m.TileHeight <= 0 || m.Width <= 0 || m.Height <= 0 {
		return nil, errNotMap
	}
	return &m, nil
}

// Load reads one map file; errors name the file.
func Load(path string) (*Map, error) {
	file := filepath.Base(path)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", file, err)
	}
	m, err := Parse(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", file, err)
	}
	return m, nil
}

// PixelWidth is the map's width in pixels.
func (m *Map) PixelWidth() float64 { return float64(m.Width * m.TileWidth) }

// PixelHeight is the map's height in pixels.
func (m *Map) PixelHeight() float64 { return float64(m.Height * m.TileHeight) }

// ObjectGroups are the map's object groups by name; a later duplicate wins.
func (m *Map) ObjectGroups() map[string]Layer {
	out := map[string]Layer{}
	for _, l := range m.Layers {
		if l.Type == ObjectGroup {
			out[l.Name] = l
		}
	}
	return out
}

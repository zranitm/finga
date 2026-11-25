// Command regionbake builds each region from its plan and writes its bake for the client, or a preview to design it by (large-regions.md section 3).
package main

import (
	"errors"
	"flag"
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"strings"

	"finga/internal/shared/gamedata/load"
	"finga/internal/shared/regiongen"
	"finga/internal/shared/sim"
)

func main() {
	data := flag.String("data", "data", "the data folder")
	out := flag.String("out", "", "the folder to write each region's bake in, as <id>/<hash>/")
	preview := flag.String("preview", "", "the folder to write each region's preview in, as <id>.png")
	plan := flag.String("plan", "", "one plan file to build, in place of every plan in <data>/regions")
	flag.Parse()
	if err := run(*data, *out, *preview, *plan); err != nil {
		fmt.Fprintln(os.Stderr, "regionbake:", err)
		os.Exit(1)
	}
}

// run builds each plan with the kit and the sim's rules from dataDir, and writes its bake under out and its preview under preview; out's stale bakes go first.
func run(dataDir, out, preview, plan string) error {
	if out == "" && preview == "" {
		return errors.New("-out or -preview is required")
	}
	params, err := load.Movement(dataDir)
	if err != nil {
		return err
	}
	raw, err := os.ReadFile(filepath.Join(dataDir, "kit.yaml"))
	if err != nil {
		return err
	}
	kit, err := regiongen.DecodeKit(raw)
	if err != nil {
		return fmt.Errorf("kit.yaml: %w", err)
	}
	plans := []string{plan}
	if plan == "" {
		if plans, err = filepath.Glob(filepath.Join(dataDir, "regions", "*.yaml")); err != nil {
			return err
		}
	}
	if out != "" {
		if err := clear(out); err != nil {
			return err
		}
	}
	for _, path := range plans {
		id := strings.TrimSuffix(filepath.Base(path), ".yaml")
		if err := build(path, id, kit, params, out, preview); err != nil {
			return fmt.Errorf("%s: %w", id, err)
		}
	}
	return nil
}

// clear makes out and removes each bake under it, a folder holding <hash>/region.json, and any bake half written; nothing else.
func clear(out string) error {
	if err := os.MkdirAll(out, 0o755); err != nil {
		return err
	}
	stale, err := filepath.Glob(filepath.Join(out, "*", "*", "region.json"))
	if err != nil {
		return err
	}
	half, err := filepath.Glob(filepath.Join(out, "*", "*"+partSuffix))
	if err != nil {
		return err
	}
	for _, f := range stale {
		half = append(half, filepath.Dir(filepath.Dir(f)))
	}
	for _, f := range half {
		if err := os.RemoveAll(f); err != nil {
			return err
		}
	}
	return nil
}

// partSuffix marks a bake being written: it's renamed to its hash once whole, so a crash never leaves a folder that looks complete.
const partSuffix = ".part"

func build(path, id string, kit regiongen.Kit, body sim.Params, out, preview string) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	p, err := regiongen.Decode(raw)
	if err != nil {
		return err
	}
	r, err := regiongen.Generate(p, kit, body)
	if err != nil {
		return err
	}
	if preview != "" {
		if err := writePNG(filepath.Join(preview, id+".png"), r); err != nil {
			return err
		}
	}
	if out == "" {
		return nil
	}
	b, err := r.Bake()
	if err != nil {
		return err
	}
	dir := filepath.Join(out, id, b.Folder())
	part := dir + partSuffix
	for _, f := range b.Files {
		to := filepath.Join(part, filepath.FromSlash(f.Path))
		if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
			return err
		}
		if f.Tile == nil {
			if err := os.WriteFile(to, f.Data, 0o644); err != nil { //nolint:gosec // a build artifact the web server serves
				return err
			}
			continue
		}
		if err := writeTile(to, f); err != nil {
			return err
		}
	}
	return os.Rename(part, dir)
}

func writeTile(to string, f regiongen.File) error {
	w, err := os.Create(to) //nolint:gosec // a path under the bake folder
	if err != nil {
		return err
	}
	if err := png.Encode(w, f.Tile); err != nil {
		_ = w.Close()
		return err
	}
	return w.Close()
}

func writePNG(to string, r *regiongen.Region) error {
	if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
		return err
	}
	return writeTile(to, regiongen.File{Tile: r.Preview()})
}

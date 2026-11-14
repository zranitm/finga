// Command testers keeps a fixed set of test accounts on a local stack, high level and ready to play, for testing by hand: make testers runs it on the stack's own network.
package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"finga/cmd/testers/config"
	"finga/internal/adminapi"
	"finga/internal/gamedata/load"
	"finga/internal/host"
)

func main() { os.Exit(run()) }

func run() int {
	cfg, err := host.LoadConfig[config.Config](config.Dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "testers: invalid configuration:", err)
		return 1
	}
	for _, u := range []string{cfg.Gateway, cfg.Admin} {
		if !cfg.Remote && !local(u) {
			fmt.Fprintf(os.Stderr, "testers: %s isn't a local stack; set remote: true (FINGA_REMOTE=true) to seed it anyway\n", u)
			return 1
		}
	}
	data, err := load.Dir(cfg.DataDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "testers:", err)
		return 1
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	client := &http.Client{Timeout: 30 * time.Second}
	s := seeder{data: data, gw: gateway{url: cfg.Gateway, http: client}, admin: adminapi.Client{URL: cfg.Admin, HTTP: client}, password: cfg.Password}

	failed := 0
	fmt.Printf("Test accounts on %s, each with the password %q:\n", cfg.Gateway, cfg.Password)
	for _, t := range testers {
		did, err := s.keep(ctx, t)
		if err != nil {
			failed++
			fmt.Printf("  %-14s %-13s failed: %v\n", t.username, t.name, err)
			continue
		}
		sd, _ := plan(data, t) // keep planned it already
		fmt.Printf("  %-14s %-13s %-9s level %-3d %s\n", t.username, t.name, sd.class.Name, sd.level, did)
	}
	if failed > 0 {
		fmt.Fprintf(os.Stderr, "testers: %d of %d accounts failed\n", failed, len(testers))
		return 1
	}
	return 0
}

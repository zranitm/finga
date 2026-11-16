package host_test

import (
	"os"
	"testing"
	"time"

	"gopkg.in/yaml.v3"

	"finga/config"
	"finga/internal/host"
	"finga/pkg/configload"
)

// Compose waits out the game's shutdown before SIGKILL, in dev and in prod: the drain, world's last saves and the trace flush, with a margin left.
func TestComposeWaitsOutTheShutdown(t *testing.T) {
	cfg, err := configload.Load[config.Config](configload.Options{Path: "../../config/config.yaml", EnvPrefix: host.EnvPrefix, Strict: true})
	if err != nil {
		t.Fatal(err)
	}
	grace := map[string]string{}
	for _, file := range []string{"../../../infra/docker-compose.yml", "../../../infra/deploy/compose.prod.yml"} { // prod's overlay overrides the base
		raw, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		var compose struct {
			Services map[string]struct {
				StopGrace string `yaml:"stop_grace_period"`
			} `yaml:"services"`
		}
		if err := yaml.Unmarshal(raw, &compose); err != nil {
			t.Fatalf("%s: %v", file, err)
		}
		for name, s := range compose.Services {
			if s.StopGrace != "" {
				grace[name] = s.StopGrace
			}
		}
	}
	need := cfg.ShutdownGrace + cfg.World.FlushTimeout + host.TraceFlushTimeout
	got, err := time.ParseDuration(grace["finga"])
	if err != nil {
		t.Fatalf("finga: stop_grace_period %q: %v", grace["finga"], err)
	}
	if got <= need {
		t.Errorf("finga: stop_grace_period %s, but its shutdown takes up to %s", got, need)
	}
}

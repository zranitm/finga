// Package config is the server's configuration, config/config.yaml: the sections every module shares, then one per module, each in its own file.
package config

import (
	"errors"
	"fmt"
	"time"

	"finga/internal/shared/host"
	"finga/pkg/dbx"
	"finga/pkg/observability/logger"
	"finga/pkg/observability/tracing"
)

// Config is config/config.yaml; FINGA_<PATH> overrides any key.
type Config struct {
	HTTP          HTTP            `mapstructure:"http"`
	Ops           host.OpsSection `mapstructure:"ops"`
	Admin         AdminSection    `mapstructure:"admin"`
	ShutdownGrace time.Duration   `mapstructure:"shutdown_grace" validate:"gt=0"`
	Log           logger.Config   `mapstructure:"log"`
	Tracing       tracing.Config  `mapstructure:"tracing"`
	Database      dbx.Config      `mapstructure:"database"`
	DataDir       string          `mapstructure:"data_dir" validate:"required"`
	WS            WS              `mapstructure:"ws"`
	Account       Account         `mapstructure:"account"`
	Item          Item            `mapstructure:"item"`
	World         World           `mapstructure:"world"`
	Social        Social          `mapstructure:"social"`
}

// AdminSection is the private admin API's listener (internal/shared/adminapi), off unless Enabled: it writes as any session's account, so it never faces players.
type AdminSection struct {
	Addr    string `mapstructure:"addr" validate:"required"`
	Enabled bool   `mapstructure:"enabled"`
}

// Validate checks the rules of each section that no validate tag can express.
func (c Config) Validate() error {
	return errors.Join(c.Item.Validate())
}

// Host is the settings of the process named process: cmd/finga, which runs every module, is the only one.
func (c Config) Host(process string) (host.Settings, error) {
	if process != "finga" {
		return host.Settings{}, fmt.Errorf("no process %s", process)
	}
	return host.Settings{Ops: c.Ops.Addr, ShutdownGrace: c.ShutdownGrace, Log: c.Log, Tracing: c.Tracing, DataDir: c.DataDir, Database: c.Database}, nil
}

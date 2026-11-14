// Package config is the server's configuration, config/config.yaml: the sections every module shares, then one per module.
package config

import (
	"errors"
	"fmt"
	"time"

	"finga/internal/host"
	"finga/pkg/dbx"
	"finga/pkg/observability/logger"
	"finga/pkg/observability/tracing"
	accountconfig "finga/services/account/config"
	gatewayconfig "finga/services/gateway/config"
	itemconfig "finga/services/item/config"
	socialconfig "finga/services/social/config"
	worldconfig "finga/services/world/config"
)

// Config is config/config.yaml; FINGA_<PATH> overrides any key.
type Config struct {
	HTTP          host.HTTPSection     `mapstructure:"http"`
	Ops           host.OpsSection      `mapstructure:"ops"`
	Admin         AdminSection         `mapstructure:"admin"`
	ShutdownGrace time.Duration        `mapstructure:"shutdown_grace" validate:"gt=0"`
	Log           logger.Config        `mapstructure:"log"`
	Tracing       tracing.Config       `mapstructure:"tracing"`
	Database      dbx.Config           `mapstructure:"database"`
	DataDir       string               `mapstructure:"data_dir" validate:"required"`
	Gateway       gatewayconfig.Config `mapstructure:"gateway"`
	Account       accountconfig.Config `mapstructure:"account"`
	Item          itemconfig.Config    `mapstructure:"item"`
	World         worldconfig.Config   `mapstructure:"world"`
	Social        socialconfig.Config  `mapstructure:"social"`
}

// AdminSection is the private admin API's listener (internal/adminapi), off unless Enabled: it writes as any session's account, so it never faces players.
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

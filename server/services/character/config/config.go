// Package config is the character module's section of config/config.yaml; FINGA_CHARACTER_<PATH> overrides a key.
package config

import (
	"finga/internal/host"
)

// Config is the character section; grpc, ops and dependencies run the module as its own process, until one binary runs every module.
type Config struct {
	GRPC         host.GRPCSection `mapstructure:"grpc"`
	Ops          host.OpsSection  `mapstructure:"ops"`
	Dependencies Dependencies     `mapstructure:"dependencies"`
}

// Dependencies are the addresses of the services this one calls.
type Dependencies struct {
	Item string `mapstructure:"item" validate:"required"`
}

// Package config is the migrate command's configuration: config.yaml beside
// this file, with the database it migrates and the log level (Low-level
// design, page 11). Every key can be overridden by FINGA_ and its path in
// upper case.
package config

import "finga/pkg/dbx"

// Dir is where the file lives, relative to server/, where the command runs
// (and /app in its image).
const Dir = "cmd/migrate/config"

// Config is cmd/migrate/config/config.yaml.
type Config struct {
	LogLevel string     `mapstructure:"log_level" validate:"oneof=debug info warn error"`
	Database dbx.Config `mapstructure:"database"`
}

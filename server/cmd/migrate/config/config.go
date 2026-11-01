// Package config loads the migrate command's config.yaml; FINGA_<KEY_PATH> overrides any key.
package config

import "finga/pkg/dbx"

// Dir is the config directory, relative to server/ (or /app in the image).
const Dir = "cmd/migrate/config"

// Config is cmd/migrate/config/config.yaml.
type Config struct {
	LogLevel string     `mapstructure:"log_level" validate:"oneof=debug info warn error"`
	Database dbx.Config `mapstructure:"database"`
}

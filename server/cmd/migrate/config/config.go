// Package config loads the migrate command's config.yaml; FINGA_<KEY_PATH> overrides any key.
package config

import (
	"finga/pkg/dbx"
	"finga/pkg/logger"
)

// Dir is the config directory, relative to server/ (or /app in the image).
const Dir = "cmd/migrate/config"

// Config is cmd/migrate/config/config.yaml.
type Config struct {
	Log      logger.Config `mapstructure:"log"`
	Database dbx.Config    `mapstructure:"database"`
}

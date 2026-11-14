// Package config loads the testers command's config.yaml; FINGA_<KEY_PATH> overrides any key.
package config

// Dir is the config directory, relative to server/ (or /app in the image).
const Dir = "cmd/testers/config"

// Config is cmd/testers/config/config.yaml.
type Config struct {
	DataDir string `mapstructure:"data_dir" validate:"required"`
	// Gateway is where the accounts sign up and play: loopback or a Compose service, unless Remote.
	Gateway string `mapstructure:"gateway" validate:"required,url"`
	// Admin is the game's private admin API, which the seed writes through where no player path exists: loopback or a Compose service, unless Remote.
	Admin string `mapstructure:"admin" validate:"required,url"`
	// Remote lets Gateway and Admin be any other host.
	Remote bool `mapstructure:"remote"`
	// Password is every test account's, for local stacks only.
	Password string `mapstructure:"password" validate:"required"`
}

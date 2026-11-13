// Package config loads the testers command's config.yaml; FINGA_<KEY_PATH> overrides any key.
package config

// Dir is the config directory, relative to server/ (or /app in the image).
const Dir = "cmd/testers/config"

// Config is cmd/testers/config/config.yaml.
type Config struct {
	DataDir string `mapstructure:"data_dir" validate:"required"`
	// Gateway is where the accounts sign up and play: loopback or a Compose service, unless Remote.
	Gateway string `mapstructure:"gateway" validate:"required,url"`
	// Remote lets Gateway be any other host.
	Remote bool `mapstructure:"remote"`
	// Password is every test account's, for local stacks only.
	Password     string       `mapstructure:"password" validate:"required"`
	Dependencies Dependencies `mapstructure:"dependencies"`
}

// Dependencies are the services the seed writes through where no player path exists.
type Dependencies struct {
	Account   string `mapstructure:"account" validate:"required"`
	Character string `mapstructure:"character" validate:"required"`
	Item      string `mapstructure:"item" validate:"required"`
}

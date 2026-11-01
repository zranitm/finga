package dbx

// Config is a pool's settings from a configuration file's database section.
type Config struct {
	URL      string `mapstructure:"url" validate:"required,url,startswith=postgres"`
	MaxConns int32  `mapstructure:"max_conns" validate:"min=1"`
}

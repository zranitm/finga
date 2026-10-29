package dbx

// Config is a pool's settings as a configuration file holds them, under a
// database section: the connection URL (postgres:// or postgresql://) and
// the pool's size.
type Config struct {
	URL      string `mapstructure:"url" validate:"required,url,startswith=postgres"`
	MaxConns int32  `mapstructure:"max_conns" validate:"min=1"`
}

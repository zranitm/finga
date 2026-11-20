package config

import "time"

// World is the world module's section of config/config.yaml; FINGA_WORLD_<PATH> overrides a key.
type World struct {
	TickRateHz         int           `mapstructure:"tick_rate_hz" validate:"min=1"`
	CheckpointInterval time.Duration `mapstructure:"checkpoint_interval" validate:"gt=0"`
	FlushTimeout       time.Duration `mapstructure:"flush_timeout" validate:"gt=0"`
	ItemCalls          ItemCalls     `mapstructure:"item_calls"`
	Channels           Channels      `mapstructure:"channels"`
}

// Channels are how a region's channels fill and close (docs/tech/large-regions.md section 4).
type Channels struct {
	Cap  int           `mapstructure:"cap" validate:"min=1"`
	Idle time.Duration `mapstructure:"idle" validate:"gt=0"`
}

// ItemCalls is the worker pool that runs the calls to the item module off the tick, and how many drops one grant call may carry: at most the item module's 32.
type ItemCalls struct {
	Workers  int `mapstructure:"workers" validate:"min=1"`
	BatchMax int `mapstructure:"batch_max" validate:"min=1,max=32"`
}

// TickInterval is the time between two ticks of a map.
func (c World) TickInterval() time.Duration {
	return time.Second / time.Duration(c.TickRateHz)
}

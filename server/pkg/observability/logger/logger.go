// Package logger builds the *slog.Logger every binary logs through, with zapcore encoding and writing its records.
package logger

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"go.uber.org/zap/zapcore"
)

// The formats a Config can name.
const (
	FormatJSON    = "json"    // one JSON object per line, for log collectors
	FormatConsole = "console" // coloured, tab-separated lines, for a terminal
)

// Config is the log section of a binary's configuration.
type Config struct {
	Level    string   `mapstructure:"level" validate:"oneof=debug info warn error"`
	Format   string   `mapstructure:"format" validate:"oneof=json console"`
	Source   bool     `mapstructure:"source"`
	Sampling Sampling `mapstructure:"sampling"`
}

// Sampling caps one message at one level per second: the first Initial, then every Thereafter-th; Initial 0 is off.
type Sampling struct {
	Initial    int `mapstructure:"initial" validate:"min=0"`
	Thereafter int `mapstructure:"thereafter" validate:"min=0"`
}

// New is a logger writing to w as cfg says.
func New(w io.Writer, cfg Config) (*slog.Logger, error) {
	level, err := parseLevel(cfg.Level)
	if err != nil {
		return nil, err
	}
	enc, err := encoder(cfg.Format)
	if err != nil {
		return nil, err
	}
	if cfg.Sampling.Initial < 0 || cfg.Sampling.Thereafter < 0 {
		return nil, errors.New("log sampling: initial and thereafter must not be negative")
	}
	core := zapcore.NewCore(enc, zapcore.Lock(zapcore.AddSync(w)), level)
	if cfg.Sampling.Initial > 0 {
		core = zapcore.NewSamplerWithOptions(core, time.Second, cfg.Sampling.Initial, cfg.Sampling.Thereafter)
	}
	return slog.New(NewHandler(core, HandlerOptions{AddSource: cfg.Source})), nil
}

func parseLevel(s string) (zapcore.Level, error) {
	switch strings.ToLower(s) {
	case "debug":
		return zapcore.DebugLevel, nil
	case "info":
		return zapcore.InfoLevel, nil
	case "warn":
		return zapcore.WarnLevel, nil
	case "error":
		return zapcore.ErrorLevel, nil
	}
	return 0, fmt.Errorf("unknown log level %q", s)
}

func encoder(format string) (zapcore.Encoder, error) {
	cfg := zapcore.EncoderConfig{
		TimeKey:        slog.TimeKey,
		LevelKey:       slog.LevelKey,
		MessageKey:     slog.MessageKey,
		CallerKey:      slog.SourceKey,
		LineEnding:     zapcore.DefaultLineEnding,
		EncodeLevel:    zapcore.CapitalLevelEncoder,
		EncodeTime:     zapcore.RFC3339NanoTimeEncoder,
		EncodeDuration: zapcore.NanosDurationEncoder,
		EncodeCaller:   zapcore.ShortCallerEncoder,
	}
	switch format {
	case FormatJSON:
		return zapcore.NewJSONEncoder(cfg), nil
	case FormatConsole:
		cfg.EncodeLevel = zapcore.CapitalColorLevelEncoder
		cfg.EncodeTime = zapcore.TimeEncoderOfLayout("15:04:05.000")
		cfg.EncodeDuration = zapcore.StringDurationEncoder
		return zapcore.NewConsoleEncoder(cfg), nil
	}
	return nil, fmt.Errorf("unknown log format %q", format)
}

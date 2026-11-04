package logger

import (
	"context"
	"log/slog"
	"os"
	"runtime"
	"slices"
	"sync"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// HandlerOptions tune a Handler.
type HandlerOptions struct {
	AddSource bool // write the caller's file:line
}

// Handler is a slog.Handler that encodes and writes records through a zapcore.Core.
type Handler struct {
	core   zapcore.Core
	opts   HandlerOptions
	errs   zapcore.WriteSyncer // where failed writes are reported: stderr
	groups []string            // opened by WithGroup, written once a field lands in them
}

// NewHandler writes through core.
func NewHandler(core zapcore.Core, opts HandlerOptions) *Handler {
	return &Handler{core: core, opts: opts, errs: zapcore.Lock(os.Stderr)}
}

// Enabled reports whether the core writes records at level.
func (h *Handler) Enabled(_ context.Context, level slog.Level) bool {
	return h.core.Enabled(zapLevel(level))
}

// Handle writes r unless the core's level or sampling drops it.
func (h *Handler) Handle(_ context.Context, r slog.Record) error {
	ce := h.core.Check(zapcore.Entry{Level: zapLevel(r.Level), Time: r.Time, Message: r.Message}, nil)
	if ce == nil {
		return nil
	}
	ce.ErrorOutput = h.errs
	if h.opts.AddSource && r.PC != 0 {
		frame, _ := runtime.CallersFrames([]uintptr{r.PC}).Next()
		ce.Caller = zapcore.EntryCaller{Defined: true, PC: frame.PC, File: frame.File, Line: frame.Line, Function: frame.Function}
	}
	pooled := fieldPool.Get().(*[]zapcore.Field)
	fields := *pooled
	for _, g := range h.groups {
		fields = append(fields, zap.Namespace(g))
	}
	opened := len(fields)
	r.Attrs(func(a slog.Attr) bool {
		fields = appendAttr(fields, a)
		return true
	})
	written := fields
	if len(fields) == opened {
		written = nil
	}
	ce.Write(written...)
	putFields(pooled, fields)
	return nil
}

// fieldPool lends Handle its field slices; a core may use fields only during Write.
var fieldPool = sync.Pool{New: func() any {
	s := make([]zapcore.Field, 0, 16)
	return &s
}}

func putFields(pooled *[]zapcore.Field, fields []zapcore.Field) {
	if cap(fields) > 64 {
		return
	}
	clear(fields)
	*pooled = fields[:0]
	fieldPool.Put(pooled)
}

// WithAttrs encodes attrs once, into every record of the returned handler.
func (h *Handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	fields := appendAttrs(nil, attrs)
	if len(fields) == 0 {
		return h
	}
	return &Handler{core: h.core.With(h.inGroups(fields)), opts: h.opts, errs: h.errs}
}

// WithGroup nests the attributes that follow under name.
func (h *Handler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	return &Handler{core: h.core, opts: h.opts, errs: h.errs, groups: append(slices.Clip(h.groups), name)}
}

func (h *Handler) inGroups(fields []zapcore.Field) []zapcore.Field {
	if len(fields) == 0 || len(h.groups) == 0 {
		return fields
	}
	out := make([]zapcore.Field, 0, len(h.groups)+len(fields))
	for _, g := range h.groups {
		out = append(out, zap.Namespace(g))
	}
	return append(out, fields...)
}

func zapLevel(l slog.Level) zapcore.Level {
	switch {
	case l >= slog.LevelError:
		return zapcore.ErrorLevel
	case l >= slog.LevelWarn:
		return zapcore.WarnLevel
	case l >= slog.LevelInfo:
		return zapcore.InfoLevel
	default:
		return zapcore.DebugLevel
	}
}

func appendAttrs(fields []zapcore.Field, attrs []slog.Attr) []zapcore.Field {
	for _, a := range attrs {
		fields = appendAttr(fields, a)
	}
	return fields
}

func appendAttr(fields []zapcore.Field, a slog.Attr) []zapcore.Field {
	a.Value = a.Value.Resolve()
	if a.Equal(slog.Attr{}) {
		return fields
	}
	v := a.Value
	switch v.Kind() {
	case slog.KindBool:
		return append(fields, zap.Bool(a.Key, v.Bool()))
	case slog.KindDuration:
		return append(fields, zap.Duration(a.Key, v.Duration()))
	case slog.KindFloat64:
		return append(fields, zap.Float64(a.Key, v.Float64()))
	case slog.KindInt64:
		return append(fields, zap.Int64(a.Key, v.Int64()))
	case slog.KindString:
		return append(fields, zap.String(a.Key, v.String()))
	case slog.KindTime:
		return append(fields, zap.Time(a.Key, v.Time()))
	case slog.KindUint64:
		return append(fields, zap.Uint64(a.Key, v.Uint64()))
	case slog.KindGroup:
		group := object(appendAttrs(nil, v.Group()))
		switch {
		case len(group) == 0:
			return fields
		case a.Key == "":
			return append(fields, zap.Inline(group))
		default:
			return append(fields, zap.Object(a.Key, group))
		}
	default:
		if err, ok := v.Any().(error); ok {
			return append(fields, zap.NamedError(a.Key, err))
		}
		return append(fields, zap.Any(a.Key, v.Any()))
	}
}

// object is a slog group's fields, encoded as one zap object.
type object []zapcore.Field

// MarshalLogObject adds the group's fields to enc.
func (o object) MarshalLogObject(enc zapcore.ObjectEncoder) error {
	for _, f := range o {
		f.AddTo(enc)
	}
	return nil
}

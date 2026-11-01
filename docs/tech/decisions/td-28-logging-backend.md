# TD-28. Logging backend

- **Area:** Operations
- **Decision:** TD-28. Logging backend
- **Default:** `log/slog`'s JSON handler (step 01)
- **Outcome:** `log/slog` as the interface and zapcore as the backend, through a handler of our own (`pkg/logger`), decided by the user on 2026-11-01. This was chosen over slog's JSON handler, over zap's own bridge `zapslog` (in `go.uber.org/zap/exp`, pre-1.0, no release since October 2024), and over calling zap directly.
- **Status:** Decided

## Why

- **The standard interface:** every package takes a `*slog.Logger`, the type libraries accept, so no package but `pkg/logger` imports zap (depguard `zap-behind-slog`) and the backend can change in one place.
- **zapcore's features:** per-message sampling, a console format for a terminal, and cores that can be teed to several outputs later. slog's JSON handler has none of these.
- **Our own handler:** under 200 lines instead of a pre-1.0 dependency. It passes the standard library's conformance suite, `testing/slogtest`.

## How it works

- **Configuration:** `logger.Config`, a binary's `log` section: `level`, `format` (`json` or `console`), `source` (file and line), and `sampling` (the first `initial` records of one message per second, then every `thereafter`-th).
- **slog's keys:** JSON lines keep `time`, `level`, `msg` and `source`, upper-case levels and nanosecond durations, so tools reading the logs see the same fields; times are RFC 3339 to the nanosecond.
- **Groups:** a slog group becomes a zap namespace only once a field lands in it, so an empty group writes nothing, as slog requires.
- **Cost:** no allocation per record, since field slices are pooled (a core may use them only during its Write). One line with six attributes takes about 560 ns, against 490 ns for slog's JSON handler (`BenchmarkHandlers`).

## Affects

Low-level design pages 1 and 11, the HLD's building blocks, the README's configuration table.

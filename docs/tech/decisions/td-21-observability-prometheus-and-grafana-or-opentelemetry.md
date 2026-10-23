# TD-21. Observability: Prometheus and Grafana, or OpenTelemetry?

- **Area:** Operations
- **Decision:** TD-21. Observability: Prometheus and Grafana, or OpenTelemetry?
- **Default:** Prometheus and Grafana, as in the HLD
- **Status:** Open

## Options

- **A. Prometheus and Grafana:** a `/metrics` endpoint scraped by Prometheus; logs as JSON on stdout. The current HLD.
- **B. OpenTelemetry with `grafana/otel-lgtm`, as in docviewer:** traces, metrics, and logs over OTLP into one container (Grafana, Tempo, Prometheus, Loki). `pkg/otelsetup` and the redaction packages can be copied from docviewer.

## Trade-off

- A is the most common setup and the smallest to start.
- B adds tracing from day one, which would show a market purchase from WebSocket message to database commit, and it reuses code that already exists and is tested.

## Affects

HLD sections 3, 4, 11, and 14; Low-level design pages 1 and 3; the Stage 1 temporary choices.

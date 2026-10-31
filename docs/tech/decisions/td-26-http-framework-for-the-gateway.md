# TD-26. HTTP framework for the gateway

- **Area:** Architecture
- **Decision:** TD-26. HTTP framework for the gateway
- **Default:** Gin
- **Outcome:** Gin, decided by the user on 2026-10-31, over the standard library's `net/http` router that the gateway was first built on (as in docviewer) and over chi and Echo. The most widely used Go web framework, so the repository shows the stack most Go teams run; the REST API is where a framework's routing groups and middleware chain read best.
- **Status:** Decided

## Where it is used

The gateway's HTTP edge only: one Gin engine (`services/gateway/internal/handler`) serves the REST API under `/api` as a route group, with an authenticated subgroup, and mounts the WebSocket at `/ws`, `/metrics` and the probes. The application layer never sees Gin (depguard `gin-at-the-edge`); the handlers adapt a request to one Lobby use case. The internal services speak gRPC and use no HTTP framework.

## How it is configured

- **Release mode, no redirects:** a path that is not a route answers 404 (`no_such_endpoint`) rather than a redirect, and a known path asked with another method answers 405 (`method_not_allowed`).
- **No trusted proxy:** `ClientIP` would otherwise read `X-Forwarded-For` from anyone; the gateway takes the socket's address for the login throttle until Stage 2's edge (decision 253).
- **The API's own middleware in place of Gin's defaults:** the request log on `log/slog` with panic recovery (Gin's logger and recovery write their own formats), CORS for the configured origins, bearer authentication. They run for unmatched routes too, so a CORS preflight is answered for any path.
- **The strict decoder stays:** request bodies are read with the standard library's JSON decoder, rejecting unknown fields, trailing data and bodies over 64 KiB, which Gin's binding does not do (it accepts trailing data, and its unknown-field switch is global). Responses are `application/json` exactly, as page 4 specifies, rather than Gin's JSON render with a charset.

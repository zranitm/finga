# TD-12. Edge proxy

- **Area:** Operations
- **Decision:** TD-12. Edge proxy
- **Default:** nginx
- **Outcome:** nginx, over Caddy and Traefik. The most widely used in industry, so the skill transfers best.
- **Status:** Decided

## Responsibilities

TLS to the origin, static files, routing for `/api` and `/ws`, per-IP limits, and WebSocket upgrade.

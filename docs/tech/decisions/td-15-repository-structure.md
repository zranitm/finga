# TD-15. Repository structure

- **Area:** Architecture
- **Decision:** TD-15. Repository structure
- **Default:** Monorepo
- **Outcome:** One monorepo with one go.mod. Generic libraries in /pkg are shared by every binary; domain code stays in /internal.
- **Status:** Decided

## Rule

`/pkg` holds only generic, reusable code and never imports `/internal`, so shared code never couples services through domain logic.

## Reconfirmed

On 2026-10-29, after the services moved into layers (Low-level design, page 11), the user was asked whether each service should get its own `go.mod` and kept one module. The services stay apart without it: Go's `internal/` rule, the depguard rules, `make test-service` and CI's per-service job, and an image per service. What one module costs is one version of each dependency for every service and no separate versioning; if a service ever needs either, it gets its own module then, with the shared code (`pkg/`, the shared kernel, the protocol, `api/gen`) as a module of its own and a `go.work` to tie them.

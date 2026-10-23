# TD-15. Repository structure

- **Area:** Architecture
- **Decision:** TD-15. Repository structure
- **Default:** Monorepo
- **Outcome:** One monorepo with one go.mod. Generic libraries in /pkg are shared by every binary; domain code stays in /internal.
- **Status:** Decided

## Rule

`/pkg` holds only generic, reusable code and never imports `/internal`, so shared code never couples services through domain logic.

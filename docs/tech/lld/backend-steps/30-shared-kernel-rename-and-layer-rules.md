# 30. Shared kernel rename and layer rules

- **Depends on:** 29
- **Features:** none (structure only)
- **Milestone:** Layers
- **Order:** 30
- **Status:** Ready
- **Step:** 30. Shared kernel rename and layer rules

**Goal:** the name `model` is free for each service's own layer, and the import direction between the layers of page 11 is checked before any service moves.

**Layers in this step:** the shared kernel and the lint rules.

## Scope

- `internal/model` becomes `internal/kernel` (`kernel.CharacterID`, `kernel.Class`), with every import updated; page 1 and page 10 name it.
- `.golangci.yml`: one depguard rule per layer of page 11 (`model`, `application`, `repository`, `handler`, `clients`, `config`) for files under `services/*/internal/<layer>/`, so they hold as each service moves in steps 31 to 36.
- Page 1's Layers and responsibilities and Module anatomy describe the layers as packages.

## Tests first

- each rule rejects the import it forbids, checked by hand when the rules are added: a forbidden import put in a layer (`application` importing `repository`, `model` importing gRPC, `handler` importing pgx) is reported by `make lint`, then removed; the log row records it

## Done when

- [ ] `make test`, `make lint`, `make cover` green; CI green.

## Review focus

- The rules forbid what page 11's table forbids, no more.

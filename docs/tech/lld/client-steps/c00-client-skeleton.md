# C00. Client skeleton

- **Depends on:** B17 (published backend)
- **Milestone:** Foundations
- **Order:** 0
- **Status:** Done
- **Step:** C00. Client skeleton

**Goal:** a `web/` project that builds, lints, tests and runs empty, wired into the Makefile and CI, so every later step adds code to a green tree.

**Layer in this step:** none of page 7's layers; tooling only.

## Scope

- `web/` laid out as page 7 shows: `index.html`, `package.json` with the lockfile, `.nvmrc` (Node 24), strict `tsconfig.json` with the `@/` alias, one `vite.config.ts` (dev proxy for `/api` and `/ws` to `:8080`, Phaser in its own chunk), `eslint.config.js` (flat, `recommendedTypeChecked`, `stylisticTypeChecked`, React hooks, `import-x/no-restricted-paths` with page 7's five layers), `.prettierrc`.
- Pinned versions from page 7: Phaser 4.x, TypeScript 6.x, Vite 8, React 19, Zustand 5, Vitest 4, ESLint 9. None of the template leftovers page 7 lists (`log.js`, second Vite config, terser, `.eslintrc.cjs`).
- Scripts: `dev`, `build`, `preview`, `typecheck`, `lint`, `format`, `test`, `coverage`; `predev` and `prebuild` copy `../data/maps` into `public/maps` (already git-ignored).
- `src/main.tsx` mounting an empty `App`; one placeholder test so `test` exits 0.
- Makefile: `test`, `lint` and `cover` also run the client's scripts through `npm --prefix web`; `cover` enforces the 80% floor from page 7 on `lib`, `protocol`, `sim`, `net`, `state`, `game/systems` once those folders exist.
- CI: a Node job with `npm ci`, `typecheck`, `lint`, `test`, `build`, cached by the lockfile.
- `README.md` gains a Client section: `make up` for the server, `npm --prefix web run dev` for the client at `:5173`.

## Design notes

- The dev proxy makes the client same-origin with the server, so `cors_origins` stays empty in Stage 1 and `VITE_API_ORIGIN` stays unset.
- The layer rule is enforced from day one, with an empty allow-list that steps extend as folders appear.

## Tests first

- a placeholder `lib` test, so `make test` runs Vitest end to end
- a lint fixture proving `import-x/no-restricted-paths` rejects an import from a lower layer (kept as a negative test file that ESLint must flag, run by a small script in `lint`)

## Done when

- [ ] `make test`, `make lint`, `make cover` and `npm --prefix web run build` pass locally and in CI.
- [ ] `npm --prefix web run dev` serves the empty app at `:5173` with `/api/healthz` proxied to the server.

## Review focus

- Versions pinned exactly as page 7 says, and no network call on build (no `log.js`).
- The Go side of `make test` is unchanged.

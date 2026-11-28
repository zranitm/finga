# test, lint and cover run server/ then web/; CI runs the -go and -web halves separately.
GO        ?= go
SERVER    ?= server
COVER_MIN ?= 80
# The game's Compose project: finga, or COMPOSE_PROJECT_NAME for a second stack beside it (infra/README.md).
GAME_PROJECT ?= $(or $(COMPOSE_PROJECT_NAME),finga)
COMPOSE   ?= docker compose -p $(GAME_PROJECT) -f infra/docker-compose.yml
# The observability stack as its own Compose project, <game>-obs, on the game's network; its host port comes from infra/.env too.
OBS_COMPOSE ?= FINGA_GAME_PROJECT=$(GAME_PROJECT) docker compose -p $(GAME_PROJECT)-obs -f infra/obs/compose.yml -f infra/obs/standalone.yml $(if $(wildcard infra/.env),--env-file infra/.env)
SMOKE_URL ?= http://localhost:8080
EDGE_URL ?= http://localhost:4173
METRICS_URL ?= http://localhost:9100
# How long make bots plays: 30m is the proof session; bots promote about half an hour in, so a longer run sees all four do it.
BOTS_DURATION ?= 30m
BOTS ?= 4
REGION ?=
NPM       ?= npm --prefix web
# The build's version, which logs and spans carry: the nearest tag and the commits since, or the commit alone.
VERSION   ?= $(shell git describe --tags --always --dirty --abbrev=12 2>/dev/null || echo dev)

.PHONY: up config prod-check down run test test-go test-web rules-go cover cover-go cover-go-cover cover-go-race race-shards cover-web lint lint-go lint-web fma-check breaking gen region-preview smoke edge bots testers recipes-check

# OBS=1 also runs the observability stack and sends it the game's traces: Grafana at http://localhost:3000.
OBSERVED = $(if $(OBS),FINGA_TRACING_OTLP_ENDPOINT=alloy:4317)

up:            ## everything in Docker: Postgres, the migrations, the game, and the client on :4173 (nginx, /api and /ws to the game); OBS=1 adds Grafana, Mimir, Loki and Tempo
	VERSION=$(VERSION) $(COMPOSE) build
	@ids=$$(docker ps --filter label=com.docker.compose.project=$(GAME_PROJECT) --format '{{.ID}} {{.Label "com.docker.compose.service"}}' | awk '$$2 != "postgres" { print $$1 }'); \
	[ -z "$$ids" ] || docker stop -t 30 $$ids # the game before Postgres, services this file no longer lists included, so their last saves land
	$(COMPOSE) stop # then Postgres: the game's project alone, so a running obs stack still collects its last logs and spans
	$(if $(OBS),,$(OBS_COMPOSE) stop)
	$(OBSERVED) $(COMPOSE) up -d --remove-orphans # orphans: obs containers from when the stack was in this project
	$(if $(OBS),$(OBS_COMPOSE) up -d && $(OBS_COMPOSE) up -d --force-recreate --no-deps alloy # after the game: Alloy joins its network. Alloy is recreated as Compose sees no change inside its bind-mounted config file)
	@echo "the game: http://localhost:$$($(COMPOSE) port web 80 | sed 's|.*:||') (or this machine's LAN address); its API: http://localhost:$$($(COMPOSE) port finga 8080 | sed 's|.*:||')"

config:        ## print the Compose configuration make up runs (FORMAT=json for JSON); OBS=1 adds the obs stack's
	$(COMPOSE) config --format $(or $(FORMAT),yaml)
	$(if $(OBS),$(OBS_COMPOSE) config --format $(or $(FORMAT),yaml))

prod-check:    ## production's Compose config, rendered from infra/deploy/prod.env as the deploy does, keeps the admin API off and unreachable (CI runs it)
	@infra/deploy/prod-check.sh

down:          ## stop everything and drop the database volume (and containers of services the Compose file no longer has)
	$(OBS_COMPOSE) down -v --remove-orphans # first: Alloy holds a place on the game's network
	$(COMPOSE) down -v --remove-orphans

# run exports server/.env as Compose's env_file does, and turns on the admin API (on loopback) for go run ./cmd/testers.
run:           ## without Docker: server/.env exported, every module's migrations, then the game
	cd $(SERVER) && set -a && if [ -f .env ]; then . ./.env; fi && set +a && $(GO) run ./cmd/migrate && \
	$(GO) build -ldflags "-X finga/internal/shared/host.version=$(VERSION)" -o bin/ ./cmd/finga && \
	FINGA_ADMIN_ENABLED=$${FINGA_ADMIN_ENABLED:-true} ./bin/finga

test: test-go test-web   ## every Go test with the race detector, then the client tests

# Every package but test/rules, the end-to-end tests first: they run in real time, so go starts them while it still builds the rest.
GO_LIST = $(GO) list ./test/... ./... | grep -vx finga/test/rules
GO_PKGS = $$($(GO_LIST))
# The race run is every package but those that start no goroutine, where the detector finds nothing and the region checks take minutes: nav's. TestTheRaceRunSkipsOnlyPackagesThatStartNoGoroutine holds each to it.
GO_RACE_SKIP := finga/internal/shared/nav
GO_RACE_PKGS = $$($(GO_LIST) | grep -vxF $(foreach p,$(GO_RACE_SKIP),-e $(p)))
# The end-to-end tests mostly wait in real time, so more of them run at once than there are CPUs.
GO_PARALLEL ?= 8
# One Postgres for the whole run, which every pgtest package shares, instead of a container each; a PGTEST_URL already set names your own.
PGTEST = $(GO) build -o bin/ ./cmd/pgtest && ./bin/pgtest

test-go:
	cd $(SERVER) && $(PGTEST) $(GO) test -race -parallel $(GO_PARALLEL) $(GO_PKGS)
	@$(MAKE) --no-print-directory rules-go

# The rules read the code, so they run plain: -race and coverage slow their type-checking and find nothing more.
rules-go:
	cd $(SERVER) && $(PGTEST) $(GO) test ./test/rules/

test-web:
	$(NPM) test

# Go coverage counts every test in the module, end-to-end ones included, minus generated stringer output.
cover: cover-go cover-web   ## coverage for both halves; each fails below its 80% floor

# cover-go runs every test twice, apart: with coverage and no race detector, then with the race detector and no coverage. Together they cost far less than one run with both, as the detector checks each of coverage's atomic counters: 36x slower than plain in the region generation. CI runs each as a job of its own.
cover-go: cover-go-cover cover-go-race

# cover-go-cover also writes server/go-test-times.tsv: when each package and test ran, slowest first.
cover-go-cover:
	cd $(SERVER) && $(GO) build -o bin/ ./cmd/testtimes && $(PGTEST) ./bin/testtimes -o go-test-times.tsv $(GO) test -json -parallel $(GO_PARALLEL) -coverpkg=./config/...,./internal/...,./pkg/...,./cmd/migrate/app/... -coverprofile=coverage.out $(GO_PKGS)
	@cd $(SERVER) && grep -v '_string.go' coverage.out > coverage.tmp && mv coverage.tmp coverage.out
	@cd $(SERVER) && $(GO) tool cover -func=coverage.out | tail -1
	@total=$$(cd $(SERVER) && $(GO) tool cover -func=coverage.out | tail -1 | awk '{print $$3}' | tr -d '%'); \
	awk -v t="$$total" -v m="$(COVER_MIN)" 'BEGIN { if (t+0 < m+0) { printf "coverage %.1f%% is below the %d%% floor\n", t, m; exit 1 } printf "coverage %.1f%% meets the %d%% floor\n", t, m }'

# The race run, writing its own times file like cover-go-cover's; CI runs it as a job.
cover-go-race:
	cd $(SERVER) && $(GO) build -o bin/ ./cmd/testtimes && $(PGTEST) ./bin/testtimes -o go-race-times.tsv $(GO) test -json -race -parallel $(GO_PARALLEL) $(GO_RACE_PKGS)

# The packages the race run takes and those it skips, one a line: the rule test reads it to check that together they are every package of GO_PKGS, once each.
race-shards:
	@cd $(SERVER) && for p in $(GO_RACE_SKIP); do echo "skipped $$p"; done; for p in $(GO_RACE_PKGS); do echo "race $$p"; done

cover-web:
	$(NPM) run coverage

lint: lint-go lint-web   ## golangci-lint and buf lint, then the client: eslint, prettier, the layer-rule fixture

lint-go:
	cd $(SERVER) && golangci-lint run ./... && $(GO) tool buf lint

lint-web:
	$(NPM) run lint

# buf compares server/api/proto with the same files at BREAKING_AGAINST (CI fetches it).
BREAKING_AGAINST ?= HEAD~1
breaking:      ## buf breaking: the client contract against BREAKING_AGAINST (default HEAD~1)
	cd $(SERVER) && $(GO) tool buf breaking --against '../.git#ref=$(BREAKING_AGAINST),subdir=server'

smoke:         ## against a running stack (make up): a client signs up, enters the world and moves
	cd $(SERVER) && FINGA_SMOKE_URL=$(SMOKE_URL) $(GO) test -count=1 -run TestSmoke ./test/

# Uses up the address's login allowance at nginx for a minute: run it last.
edge:          ## against a running stack (make up): nginx limits per client address, the server does not
	cd $(SERVER) && FINGA_EDGE_URL=$(EDGE_URL) FINGA_SERVER_URL=$(SMOKE_URL) $(GO) test -count=1 -run TestEdge ./test/

# A .PHONY target left with no recipe passes in silence: #291 dropped four that way, and CI ran none of them for 16 merges.
recipes-check: ## every .PHONY target does something under make -n (CI runs it)
	@for t in $$(sed -n 's/^\.PHONY://p' $(firstword $(MAKEFILE_LIST))); do \
	  [ "$$t" = recipes-check ] && continue; \
	  out=$$($(MAKE) -n --no-print-directory $$t 2>&1) || { echo "$$t: $$out"; exit 1; }; \
	  if [ -z "$$out" ] || printf '%s\n' "$$out" | grep -q 'Nothing to be done'; then echo "$$t: listed in .PHONY with no recipe"; exit 1; fi; \
	done; echo "every .PHONY target has a recipe"

# The packages whose numbers are the same bits on every CPU: each product rounded on its own (float64(a*b)), so arm64 can't fuse it into an add.
FMA_PKGS := sim mapfile nav regiongen
fma-check:     ## those packages compile to no fused multiply-add on arm64 (CI runs it)
	@cd $(SERVER) && mkdir -p bin && for p in $(FMA_PKGS); do \
	  GOARCH=arm64 $(GO) build -o bin/fma-$$p.a ./internal/shared/$$p/ || exit 1; \
	  asm=$$($(GO) tool objdump bin/fma-$$p.a) || exit 1; \
	  funcs=$$(printf '%s\n' "$$asm" | grep -c "^TEXT finga/internal/shared/$$p\."); \
	  fused=$$(printf '%s\n' "$$asm" | grep -cE '\sF(N)?M(ADD|SUB)D\s'); \
	  if [ "$$funcs" = 0 ]; then echo "internal/shared/$$p: no function of it disassembled, so nothing was checked"; exit 1; fi; \
	  if [ "$$fused" != 0 ]; then echo "internal/shared/$$p: $$fused fused multiply-adds on arm64; round each product with float64(a*b)"; exit 1; fi; \
	  echo "internal/shared/$$p: $$funcs functions on arm64, none fused"; \
	done

region-preview: ## a region from above to design it by, areas and camps marked: every plan in server/data/regions, or PLAN=path/to/plan.yaml; PNGs land in server/bake/preview
	cd $(SERVER) && $(GO) run ./cmd/regionbake -data data -preview bake/preview $(if $(PLAN),-plan $(abspath $(PLAN)))

# Run after a query, migration or contract change.
gen:           ## regenerate the sqlc queries and the Go code of the client contract
	cd $(SERVER) && $(GO) tool sqlc generate && $(GO) tool buf generate

bots:          ## the proof session: BOTS (4) bots play for BOTS_DURATION (30m) against SMOKE_URL, each working toward its own first class, or with REGION a soak, hunting in that region alone; then print the promotions, each bot's downstream kB/s, the tick p99 by map and channel, grant batch sizes and AddMany times from the game's ops address (METRICS_URL)
	cd $(SERVER) && $(GO) run ./cmd/bot -url $(SMOKE_URL) -metrics $(METRICS_URL) -bots $(BOTS) -region "$(REGION)" -duration $(BOTS_DURATION)

testers:       ## the test accounts (README), kept on the running stack from its own network; a stack that has them changes nothing
	$(COMPOSE) --profile tools run --rm --build --no-deps testers

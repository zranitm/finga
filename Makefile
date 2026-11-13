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
METRICS_URL ?= http://localhost:9104
ITEM_METRICS_URL ?= http://localhost:9103
# How long make bots plays: 30m is the proof session; bots promote about half an hour in, so a longer run sees all four do it.
BOTS_DURATION ?= 30m
NPM       ?= npm --prefix web
# The build's version, which logs and spans carry: the nearest tag and the commits since, or the commit alone.
VERSION   ?= $(shell git describe --tags --always --dirty --abbrev=12 2>/dev/null || echo dev)

.PHONY: up config down run test test-go test-web test-service cover cover-go cover-web lint lint-go lint-web breaking gen smoke edge bots testers

# OBS=1 also runs the observability stack and sends it the services' traces: Grafana at http://localhost:3000.
OBSERVED = $(if $(OBS),FINGA_TRACING_OTLP_ENDPOINT=alloy:4317)

up:            ## everything in Docker: Postgres, the migrations, the six services, and the client on :4173 (nginx, /api and /ws to the gateway); OBS=1 adds Grafana, Mimir, Loki and Tempo
	VERSION=$(VERSION) $(COMPOSE) build
	$(COMPOSE) stop # the game alone first, so a running obs stack still collects its last logs and spans
	$(if $(OBS),,$(OBS_COMPOSE) stop)
	$(OBSERVED) $(COMPOSE) up -d --remove-orphans # orphans: obs containers from when the stack was in this project
	$(if $(OBS),$(OBS_COMPOSE) up -d # after the game: Alloy joins its network)
	@echo "the game: http://localhost:$$($(COMPOSE) port web 80 | sed 's|.*:||') (or this machine's LAN address); the gateway: http://localhost:$$($(COMPOSE) port gateway 8080 | sed 's|.*:||')"

config:        ## print the Compose configuration make up runs (FORMAT=json for JSON); OBS=1 adds the obs stack's
	$(COMPOSE) config --format $(or $(FORMAT),yaml)
	$(if $(OBS),$(OBS_COMPOSE) config --format $(or $(FORMAT),yaml))

down:          ## stop everything and drop the database volume (and containers of services the Compose file no longer has)
	$(OBS_COMPOSE) down -v --remove-orphans # first: Alloy holds a place on the game's network
	$(COMPOSE) down -v --remove-orphans

# run exports server/.env as Compose's env_file does; the services stop with the gateway on Ctrl-C.
SERVICES ?= account character item social world
run:           ## without Docker: server/.env exported, every service's migrations, then the six services
	cd $(SERVER) && set -a && if [ -f .env ]; then . ./.env; fi && set +a && $(GO) run ./cmd/migrate && \
	$(GO) build -ldflags "-X finga/internal/host.version=$(VERSION)" -o bin/ ./services/gateway $(foreach s,$(SERVICES),./services/$(s)) && \
	trap 'kill 0' EXIT; for s in $(SERVICES); do ./bin/$$s & done; ./bin/gateway

test: test-go test-web   ## every Go test with the race detector, then the client tests

test-go:
	cd $(SERVER) && $(GO) test -race ./...

test-web:
	$(NPM) test

# One service on its own, as CI's matrix runs it.
test-service:  ## build and test one service: make test-service SERVICE=<gateway|account|character|item|world|social>
	@test -n "$(SERVICE)" || { echo "usage: make test-service SERVICE=<name>"; exit 1; }
	@test -d $(SERVER)/services/$(SERVICE) || { echo "no service $(SERVICE) under $(SERVER)/services"; exit 1; }
	cd $(SERVER) && $(GO) build ./services/$(SERVICE)/... && $(GO) test -race ./services/$(SERVICE)/...

# Go coverage counts every test in the module, end-to-end ones included, minus generated stringer output.
cover: cover-go cover-web   ## coverage for both halves; each fails below its 80% floor

# Tests cover-go runs apart, under -race alone: the race detector slows coverage's atomic counters 6-14x in their loops, and they cover nothing other tests don't.
RACE_ONLY_TESTS := TestEveryPortalSpawnAndBossCanBeReached|TestBalance|TestFieldBosses
RACE_ONLY_PKGS := ./internal/gamedata/load/ ./services/world/internal/model/

cover-go:
	cd $(SERVER) && $(GO) test -race -coverpkg=./internal/...,./pkg/...,./services/...,./cmd/migrate/app/... -coverprofile=coverage.out -skip '^($(RACE_ONLY_TESTS))$$' ./...
	cd $(SERVER) && $(GO) test -race -run '^($(RACE_ONLY_TESTS))$$' $(RACE_ONLY_PKGS)
	@cd $(SERVER) && grep -v '_string.go' coverage.out > coverage.tmp && mv coverage.tmp coverage.out
	@cd $(SERVER) && $(GO) tool cover -func=coverage.out | tail -1
	@total=$$(cd $(SERVER) && $(GO) tool cover -func=coverage.out | tail -1 | awk '{print $$3}' | tr -d '%'); \
	awk -v t="$$total" -v m="$(COVER_MIN)" 'BEGIN { if (t+0 < m+0) { printf "coverage %.1f%% is below the %d%% floor\n", t, m; exit 1 } printf "coverage %.1f%% meets the %d%% floor\n", t, m }'

cover-web:
	$(NPM) run coverage

lint: lint-go lint-web   ## golangci-lint and buf lint, then the client: eslint, prettier, the layer-rule fixture

lint-go:
	cd $(SERVER) && golangci-lint run ./... && $(GO) tool buf lint

lint-web:
	$(NPM) run lint

# buf compares server/api/proto with the same files at BREAKING_AGAINST (CI fetches it).
BREAKING_AGAINST ?= HEAD~1
breaking:      ## buf breaking: the gRPC and client contracts against BREAKING_AGAINST (default HEAD~1)
	cd $(SERVER) && $(GO) tool buf breaking --against '../.git#ref=$(BREAKING_AGAINST),subdir=server'

smoke:         ## against a running stack (make up): a client signs up, enters the world and moves
	cd $(SERVER) && FINGA_SMOKE_URL=$(SMOKE_URL) $(GO) test -count=1 -run TestSmoke ./test/

# Uses up the address's login allowance at nginx for a minute: run it last.
edge:          ## against a running stack (make up): nginx limits per client address, the gateway does not
	cd $(SERVER) && FINGA_EDGE_URL=$(EDGE_URL) FINGA_GATEWAY_URL=$(SMOKE_URL) $(GO) test -count=1 -run TestEdge ./test/

# Run after a query, migration or contract change.
gen:           ## regenerate the sqlc queries and the Go code of the gRPC and client contracts
	cd $(SERVER) && $(GO) tool sqlc generate && $(GO) tool buf generate

bots:          ## the proof session: 4 bots play for BOTS_DURATION (30m) against SMOKE_URL, each working toward its own first class, and print the promotions, the tick p99 and grant batch sizes from the world (METRICS_URL) and AddMany times from the item service (ITEM_METRICS_URL)
	cd $(SERVER) && $(GO) run ./cmd/bot -url $(SMOKE_URL) -metrics $(METRICS_URL) -item-metrics $(ITEM_METRICS_URL) -bots 4 -duration $(BOTS_DURATION)

testers:       ## the test accounts (README), kept on the running stack from its own network; a stack that has them changes nothing
	$(COMPOSE) --profile tools run --rm --build --no-deps testers

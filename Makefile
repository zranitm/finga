# test, lint and cover run server/ then web/; CI runs the -go and -web halves separately.
GO        ?= go
SERVER    ?= server
COVER_MIN ?= 80
COMPOSE   ?= docker compose -f infra/docker-compose.yml
SMOKE_URL ?= http://localhost:8080
EDGE_URL ?= http://localhost:4173
METRICS_URL ?= http://localhost:9104
NPM       ?= npm --prefix web

.PHONY: up down run test test-go test-web test-service cover cover-go cover-web lint lint-go lint-web breaking gen smoke edge bots

# OBS=1 also runs the observability stack and sends it the services' traces: Grafana at http://localhost:3000.
OBSERVED = $(if $(OBS),COMPOSE_PROFILES=obs FINGA_TRACING_OTLP_ENDPOINT=alloy:4317)

up:            ## everything in Docker: Postgres, the migrations, the six services, and the client on :4173 (nginx, /api and /ws to the gateway); OBS=1 adds Grafana, Mimir, Loki and Tempo
	$(OBSERVED) $(COMPOSE) build
	$(COMPOSE) stop # the game alone first, so a running obs stack still collects its last logs and spans
	$(if $(OBS),,COMPOSE_PROFILES=obs $(COMPOSE) stop)
	$(OBSERVED) $(COMPOSE) up -d
	@echo "the game: http://localhost:$$($(COMPOSE) port web 80 | sed 's|.*:||') (or this machine's LAN address); the gateway: http://localhost:$$($(COMPOSE) port gateway 8080 | sed 's|.*:||')"

down:          ## stop everything and drop the database volume (and containers of services the Compose file no longer has)
	COMPOSE_PROFILES=obs $(COMPOSE) down -v --remove-orphans

# run exports server/.env as Compose's env_file does; the services stop with the gateway on Ctrl-C.
SERVICES ?= account character item social world
run:           ## without Docker: server/.env exported, every service's migrations, then the six services
	cd $(SERVER) && set -a && if [ -f .env ]; then . ./.env; fi && set +a && $(GO) run ./cmd/migrate && \
	$(GO) build -o bin/ ./services/gateway $(foreach s,$(SERVICES),./services/$(s)) && \
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

cover-go:
	cd $(SERVER) && $(GO) test -race -coverpkg=./internal/...,./pkg/...,./services/...,./cmd/migrate/app/... -coverprofile=coverage.out ./...
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

bots:          ## the proof session: 4 bots play for 30 minutes against SMOKE_URL and print the tick p99 from the world (METRICS_URL)
	cd $(SERVER) && $(GO) run ./cmd/bot -url $(SMOKE_URL) -metrics $(METRICS_URL) -bots 4 -duration 30m

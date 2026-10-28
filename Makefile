# Targets the README documents: up, down, run, test, test-service, cover, lint, breaking, gen, smoke, bots, play.
# The repository has three parts: server/ (the Go module), web/ (the client)
# and infra/ (how it is run). test, lint and cover run both halves (Go inside
# server/, then the client through npm --prefix web); the -go and -web
# targets run one half, which CI uses.
GO        ?= go
SERVER    ?= server
COVER_MIN ?= 80
COMPOSE   ?= docker compose -f infra/docker-compose.yml
SMOKE_URL ?= http://localhost:8080
METRICS_URL ?= http://localhost:9104
NPM       ?= npm --prefix web

.PHONY: up down run test test-go test-web test-service cover cover-go cover-web lint lint-go lint-web breaking gen smoke bots play

up:            ## build the images and start Postgres, the migrations and the six services (the gateway on :8080)
	$(COMPOSE) up --build -d

down:          ## stop everything and drop the database volume (and containers of services the Compose file no longer has)
	$(COMPOSE) down -v --remove-orphans

# The binaries read only their environment, so run exports server/.env (when
# present) into it, the way Compose's env_file does for make up. The services
# run in the background on the defaults in configs/config.yaml (localhost
# ports) and stop with the gateway, on Ctrl-C.
SERVICES ?= account character item social world
run:           ## without Docker: server/.env exported, every service's migrations, then the six services
	cd $(SERVER) && set -a && if [ -f .env ]; then . ./.env; fi && set +a && $(GO) run ./cmd/migrate && \
	$(GO) build -o bin/ ./services/gateway/cmd/gateway $(foreach s,$(SERVICES),./services/$(s)/cmd/$(s)) && \
	trap 'kill 0' EXIT; for s in $(SERVICES); do ./bin/$$s & done; ./bin/gateway

test: test-go test-web   ## every Go test with the race detector, then the client tests

test-go:
	cd $(SERVER) && $(GO) test -race ./...

test-web:
	$(NPM) test

# One service on its own, as CI's matrix runs it: it must build and pass its
# tests without the rest of the module's tests (make test-service SERVICE=item).
test-service:  ## build and test one service: make test-service SERVICE=<gateway|account|character|item|world|social>
	@test -n "$(SERVICE)" || { echo "usage: make test-service SERVICE=<name>"; exit 1; }
	@test -d $(SERVER)/services/$(SERVICE) || { echo "no service $(SERVICE) under $(SERVER)/services"; exit 1; }
	cd $(SERVER) && $(GO) build ./services/$(SERVICE)/... && $(GO) test -race ./services/$(SERVICE)/...

# Go coverage counts statements in server/internal/, server/pkg/, server/services/
# (each service's composition root included) and server/cmd/migrate/app/, from every test in
# the module (end-to-end tests under server/test/ included). The main.go files
# are entry points, exercised end to end, and stringer output (*_string.go) is
# generated, so both are excluded. The client enforces page 7's 80% floor on its tested layers itself.
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

# The internal contracts must not break their callers: buf compares
# server/api/proto with the same files at BREAKING_AGAINST (the previous
# commit by default; CI fetches it).
BREAKING_AGAINST ?= HEAD~1
breaking:      ## buf breaking: the gRPC contracts against BREAKING_AGAINST (default HEAD~1)
	cd $(SERVER) && $(GO) tool buf breaking --against '../.git#ref=$(BREAKING_AGAINST),subdir=server'

smoke:         ## against a running stack (make up): a client signs up, enters the world and moves
	cd $(SERVER) && FINGA_SMOKE_URL=$(SMOKE_URL) $(GO) test -count=1 -run TestSmoke ./test/

# sqlc generates each module's queries into server/services/<service>/internal/<module>/internal/db;
# each entry's schema is its service's server/services/<service>/migrations/. buf generates the
# Go code of the internal contracts (server/api/proto) into server/api/gen, with the generators
# pinned as Go tools in server/go.mod. Run it after a query, migration or contract change.
gen:           ## regenerate the sqlc queries and the gRPC code
	cd $(SERVER) && sqlc generate && $(GO) tool buf generate

bots:          ## the proof session: 4 bots play for 30 minutes against SMOKE_URL and print the tick p99 from the world (METRICS_URL)
	cd $(SERVER) && $(GO) run ./cmd/bot -url $(SMOKE_URL) -metrics $(METRICS_URL) -bots 4 -duration 30m

play:          ## the proof session with people: make up, the client production build, and vite preview on the LAN (:4173)
	$(MAKE) up
	npm --prefix web run build
	npm --prefix web run preview -- --host --port 4173 --strictPort

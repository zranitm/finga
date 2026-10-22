# Targets the README documents: up, down, test, cover, lint, gen, smoke, bots.
# test, lint and cover run both halves (Go, then the client through
# npm --prefix web); the -go and -web targets run one half, which CI uses.
GO        ?= go
COVER_MIN ?= 80
COMPOSE   ?= docker compose -f deploy/docker-compose.yml
SMOKE_URL ?= http://localhost:8080
NPM       ?= npm --prefix web

.PHONY: up down test test-go test-web cover cover-go cover-web lint lint-go lint-web gen smoke bots

up:            ## build the images and start Postgres, the migrations and the game server (:8080)
	$(COMPOSE) up --build -d

down:          ## stop everything and drop the database volume
	$(COMPOSE) down -v

test: test-go test-web   ## every Go test with the race detector, then the client tests

test-go:
	$(GO) test -race ./...

test-web:
	$(NPM) test

# Go coverage counts statements in internal/ and pkg/ only, from every test in
# the module (end-to-end tests under test/ included). cmd/ is wiring, exercised
# end to end, and stringer output (*_string.go) is generated, so both are
# excluded. The client enforces page 7's 80% floor on its tested layers itself.
cover: cover-go cover-web   ## coverage for both halves; each fails below its 80% floor

cover-go:
	$(GO) test -race -coverpkg=./internal/...,./pkg/... -coverprofile=coverage.out ./...
	@grep -v '_string.go' coverage.out > coverage.tmp && mv coverage.tmp coverage.out
	@$(GO) tool cover -func=coverage.out | tail -1
	@total=$$($(GO) tool cover -func=coverage.out | tail -1 | awk '{print $$3}' | tr -d '%'); \
	awk -v t="$$total" -v m="$(COVER_MIN)" 'BEGIN { if (t+0 < m+0) { printf "coverage %.1f%% is below the %d%% floor\n", t, m; exit 1 } printf "coverage %.1f%% meets the %d%% floor\n", t, m }'

cover-web:
	$(NPM) run coverage

lint: lint-go lint-web   ## golangci-lint, then the client: eslint, prettier, the layer-rule fixture

lint-go:
	golangci-lint run ./...

lint-web:
	$(NPM) run lint

smoke:         ## against a running stack (make up): a client signs up, enters the world and moves
	FINGA_SMOKE_URL=$(SMOKE_URL) $(GO) test -count=1 -run TestSmoke ./test/

# sqlc generates each module's queries into internal/application/<module>/internal/db.
# The schema for every entry is migrations/; run it after a query or migration change.
gen:           ## regenerate sqlc code
	@if [ -f sqlc.yaml ]; then sqlc generate; else echo "gen: no sqlc.yaml yet"; fi

bots:          ## the proof session: 4 bots play for 30 minutes against SMOKE_URL and print the tick p99
	$(GO) run ./cmd/bot -url $(SMOKE_URL) -bots 4 -duration 30m

play:          ## the proof session with people: make up, the client production build, and vite preview on the LAN (:4173)
	$(MAKE) up
	npm --prefix web run build
	npm --prefix web run preview -- --host --port 4173 --strictPort

# Targets the README documents: up, down, test, cover, lint, gen, smoke, bots.
GO        ?= go
COVER_MIN ?= 80
COMPOSE   ?= docker compose -f deploy/docker-compose.yml
SMOKE_URL ?= http://localhost:8080

.PHONY: up down test cover lint gen smoke bots

up:            ## build the images and start Postgres, the migrations and the game server (:8080)
	$(COMPOSE) up --build -d

down:          ## stop everything and drop the database volume
	$(COMPOSE) down -v

test:          ## every test, with the race detector; no Docker needed
	$(GO) test -race ./...

# Coverage counts statements in internal/ and pkg/ only, from every test in the
# module (end-to-end tests under test/ included). cmd/ is wiring, exercised end
# to end, and stringer output (*_string.go) is generated, so both are excluded.
cover:         ## coverage report; fails below COVER_MIN
	$(GO) test -race -coverpkg=./internal/...,./pkg/... -coverprofile=coverage.out ./...
	@grep -v '_string.go' coverage.out > coverage.tmp && mv coverage.tmp coverage.out
	@$(GO) tool cover -func=coverage.out | tail -1
	@total=$$($(GO) tool cover -func=coverage.out | tail -1 | awk '{print $$3}' | tr -d '%'); \
	awk -v t="$$total" -v m="$(COVER_MIN)" 'BEGIN { if (t+0 < m+0) { printf "coverage %.1f%% is below the %d%% floor\n", t, m; exit 1 } printf "coverage %.1f%% meets the %d%% floor\n", t, m }'

lint:          ## golangci-lint, including constant log messages (sloglint)
	golangci-lint run ./...

smoke:         ## against a running stack (make up): a client signs up, enters the world and moves
	FINGA_SMOKE_URL=$(SMOKE_URL) $(GO) test -count=1 -run TestSmoke ./test/

# sqlc generates each module's queries into internal/application/<module>/internal/db.
# The schema for every entry is migrations/; run it after a query or migration change.
gen:           ## regenerate sqlc code
	@if [ -f sqlc.yaml ]; then sqlc generate; else echo "gen: no sqlc.yaml yet"; fi

bots:          ## the proof session: 4 bots play for 30 minutes against SMOKE_URL and print the tick p99
	$(GO) run ./cmd/bot -url $(SMOKE_URL) -bots 4 -duration 30m

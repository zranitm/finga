# Targets the README documents: up, down, test, cover, lint, gen.
GO        ?= go
COVER_MIN ?= 80
COMPOSE   ?= docker compose -f deploy/docker-compose.yml

.PHONY: up down test cover lint gen

up:            ## start Postgres (later: migrations, the game server, the client)
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

# sqlc generates each module's queries into internal/application/<module>/internal/db.
# No module has queries yet, so this is a no-op until step 02.
gen:           ## regenerate sqlc code
	@if [ -f sqlc.yaml ]; then sqlc generate; else echo "gen: no sqlc.yaml yet"; fi

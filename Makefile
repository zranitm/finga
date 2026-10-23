# Targets the README documents: up, down, run, test, cover, lint, gen, smoke, bots, play.
# The repository has three parts: server/ (the Go module), web/ (the client)
# and infra/ (how it is run). test, lint and cover run both halves (Go inside
# server/, then the client through npm --prefix web); the -go and -web
# targets run one half, which CI uses.
GO        ?= go
SERVER    ?= server
COVER_MIN ?= 80
COMPOSE   ?= docker compose -f infra/docker-compose.yml
SMOKE_URL ?= http://localhost:8080
NPM       ?= npm --prefix web

.PHONY: up down run test test-go test-web cover cover-go cover-web lint lint-go lint-web gen smoke bots play

up:            ## build the images and start Postgres, the migrations and the game server (:8080)
	$(COMPOSE) up --build -d

down:          ## stop everything and drop the database volume
	$(COMPOSE) down -v

# The binaries read only their environment, so run exports server/.env (when
# present) into it, the way Compose's env_file does for make up.
run:           ## without Docker: server/.env exported, the migrations, then the game server
	cd $(SERVER) && set -a && if [ -f .env ]; then . ./.env; fi && set +a && $(GO) run ./cmd/migrate && $(GO) run ./cmd/gameserver

test: test-go test-web   ## every Go test with the race detector, then the client tests

test-go:
	cd $(SERVER) && $(GO) test -race ./...

test-web:
	$(NPM) test

# Go coverage counts statements in server/internal/, server/pkg/ and the game
# server's composition root (server/cmd/gameserver/app/), from every test in
# the module (end-to-end tests under server/test/ included). The main.go files
# are entry points, exercised end to end, and stringer output (*_string.go) is
# generated, so both are excluded. The client enforces page 7's 80% floor on its tested layers itself.
cover: cover-go cover-web   ## coverage for both halves; each fails below its 80% floor

cover-go:
	cd $(SERVER) && $(GO) test -race -coverpkg=./internal/...,./pkg/...,./cmd/gameserver/app/... -coverprofile=coverage.out ./...
	@cd $(SERVER) && grep -v '_string.go' coverage.out > coverage.tmp && mv coverage.tmp coverage.out
	@cd $(SERVER) && $(GO) tool cover -func=coverage.out | tail -1
	@total=$$(cd $(SERVER) && $(GO) tool cover -func=coverage.out | tail -1 | awk '{print $$3}' | tr -d '%'); \
	awk -v t="$$total" -v m="$(COVER_MIN)" 'BEGIN { if (t+0 < m+0) { printf "coverage %.1f%% is below the %d%% floor\n", t, m; exit 1 } printf "coverage %.1f%% meets the %d%% floor\n", t, m }'

cover-web:
	$(NPM) run coverage

lint: lint-go lint-web   ## golangci-lint, then the client: eslint, prettier, the layer-rule fixture

lint-go:
	cd $(SERVER) && golangci-lint run ./...

lint-web:
	$(NPM) run lint

smoke:         ## against a running stack (make up): a client signs up, enters the world and moves
	cd $(SERVER) && FINGA_SMOKE_URL=$(SMOKE_URL) $(GO) test -count=1 -run TestSmoke ./test/

# sqlc generates each module's queries into server/internal/application/<module>/internal/db.
# The schema for every entry is server/migrations/; run it after a query or migration change.
gen:           ## regenerate sqlc code
	cd $(SERVER) && sqlc generate

bots:          ## the proof session: 4 bots play for 30 minutes against SMOKE_URL and print the tick p99
	cd $(SERVER) && $(GO) run ./cmd/bot -url $(SMOKE_URL) -bots 4 -duration 30m

play:          ## the proof session with people: make up, the client production build, and vite preview on the LAN (:4173)
	$(MAKE) up
	npm --prefix web run build
	npm --prefix web run preview -- --host --port 4173 --strictPort

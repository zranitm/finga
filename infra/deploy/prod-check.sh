#!/usr/bin/env bash
# Renders production's Compose config as deploy.sh does, from prod.env with stand-ins for what the host and SSM supply,
# and fails if the game's admin API could be on or reachable there: it writes as any session's account. CI runs it.
set -euo pipefail
cd "$(dirname "$0")/../.." # the release's root, where prod.env's paths start
set -a
# shellcheck source=prod.env
. infra/deploy/prod.env
REGISTRY=registry.invalid IMAGE_TAG=check DEPLOY_SHARED_DIR=/nonexistent FINGA_POSTGRES_PASSWORD=check
set +a
problems=$(docker compose config --format json | jq -r '
  (if .services.finga.environment.FINGA_ADMIN_ENABLED != "false" then "finga: FINGA_ADMIN_ENABLED is not \"false\"" else empty end),
  (if .services.finga.network_mode == "host" then "finga: on the host network" else empty end),
  (if (.services.finga.environment // {}) | has("CONFIG_PATH") then "finga: CONFIG_PATH names another config" else empty end),
  (.services.finga.volumes[]? | select(.target | startswith("/app/config")) | "finga: \(.source // "a volume") is mounted at \(.target)"),
  (.services | to_entries[] | select(any(.value.ports[]?; .target == 9200 or (.published | tostring) == "9200")) | "\(.key): publishes 9200")')
if [[ -n $problems ]]; then
	printf "production's Compose config could open the admin API:\n%s\n" "$problems" >&2
	exit 1
fi
echo "production keeps the admin API off"

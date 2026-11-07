#!/usr/bin/env bash
# Postgres dumps for a Compose project on a compose-host instance, kept in the host's artifact bucket.
#
#   pg.sh backup                       pg_dump -Fc of the running database to s3://ARTIFACT_BUCKET/BACKUP_PREFIX/YYYY/MM/DD/<time>.dump
#   pg.sh list                         the dumps' keys, oldest first
#   pg.sh restore <key|latest> --yes   pg_restore --clean into the running database: stop the services that use it first
#
# The container is found by its Compose labels (project APP_NAME, service BACKUP_POSTGRES_SERVICE), and the commands
# run inside it with the official image's POSTGRES_USER, POSTGRES_PASSWORD and POSTGRES_DB, so nothing here knows
# the application. Settings come from /etc/compose-host/host.env and <DEPLOY_ENV>.env beside this script.
set -Eeuo pipefail

HOST_ENV=${HOST_ENV:-/etc/compose-host/host.env}
tmp= # the dump on its way through this host, removed on exit
trap 'if [[ -n $tmp ]]; then rm -f "$tmp"; fi' EXIT

log() { printf '%s %s\n' "$(date -u +%H:%M:%S)" "$*"; }
die() {
	log "error: $*" >&2
	exit 1
}

load_settings() {
	[[ -r $HOST_ENV ]] || die "no $HOST_ENV: is this a compose-host instance?"
	set -a
	# shellcheck source=/dev/null
	. "$HOST_ENV"
	local app_env
	app_env=$(dirname "$(readlink -f "${BASH_SOURCE[0]}")")/${DEPLOY_ENV:-prod}.env
	# shellcheck source=/dev/null
	if [[ -r $app_env ]]; then . "$app_env"; fi
	set +a
	: "${APP_NAME:?}" "${AWS_REGION:?}" "${ARTIFACT_BUCKET:?}"
	project=${BACKUP_COMPOSE_PROJECT:-$APP_NAME}
	service=${BACKUP_POSTGRES_SERVICE:-postgres}
	prefix=${BACKUP_PREFIX:-backups/postgres}
}

container() {
	local id
	id=$(docker ps -q --filter "label=com.docker.compose.project=$project" \
		--filter "label=com.docker.compose.service=$service" | head -n 1)
	[[ -n $id ]] || die "no running $service container in Compose project $project"
	printf '%s\n' "$id"
}

cmd_backup() {
	local id key
	id=$(container)
	tmp=$(mktemp)
	# The dump goes to a file first, so a failed pg_dump never leaves a partial object in the bucket.
	# shellcheck disable=SC2016 # expanded by the shell inside the container
	docker exec "$id" sh -c 'PGPASSWORD="$POSTGRES_PASSWORD" exec pg_dump -U "$POSTGRES_USER" -d "${POSTGRES_DB:-$POSTGRES_USER}" -Fc' >"$tmp"
	[[ -s $tmp ]] || die "pg_dump wrote nothing"
	key=$prefix/$(date -u +%Y/%m/%d/%Y%m%dT%H%M%SZ).dump
	aws s3 cp --only-show-errors --region "$AWS_REGION" "$tmp" "s3://$ARTIFACT_BUCKET/$key"
	log "backed up $(du -h "$tmp" | cut -f1) to s3://$ARTIFACT_BUCKET/$key"
}

cmd_list() {
	aws s3 ls --recursive --region "$AWS_REGION" "s3://$ARTIFACT_BUCKET/$prefix/" | awk '{print $4}' | sort
}

cmd_restore() {
	local key=${1:-} id
	[[ -n $key ]] || die "usage: pg.sh restore <key|latest> --yes"
	[[ ${2:-} == --yes ]] || die "a restore replaces the database's contents: add --yes"
	if [[ $key == latest ]]; then
		key=$(cmd_list | tail -n 1)
		[[ -n $key ]] || die "no dumps under s3://$ARTIFACT_BUCKET/$prefix/"
	fi
	id=$(container)
	tmp=$(mktemp)
	aws s3 cp --only-show-errors --region "$AWS_REGION" "s3://$ARTIFACT_BUCKET/$key" "$tmp"
	log "restoring $key"
	# shellcheck disable=SC2016 # expanded by the shell inside the container
	docker exec -i "$id" sh -c 'PGPASSWORD="$POSTGRES_PASSWORD" exec pg_restore -U "$POSTGRES_USER" -d "${POSTGRES_DB:-$POSTGRES_USER}" --clean --if-exists --no-owner --exit-on-error' <"$tmp"
	log "restored $key"
}

main() {
	local cmd=${1:-}
	shift || true
	load_settings
	case $cmd in
	backup) cmd_backup ;;
	list) cmd_list ;;
	restore) cmd_restore "$@" ;;
	*)
		sed -n '2,7p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//' >&2
		exit 2
		;;
	esac
}

main "$@"

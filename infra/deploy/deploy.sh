#!/usr/bin/env bash
# Deploys one release of a Compose application on a compose-host instance, or runs Compose the way a deploy does.
#
#   deploy.sh deploy <release>    what compose-release runs once the bundle is unpacked: settings and secrets,
#                                 pull, stop, up, health checks, then the switch of DEPLOY_ROOT/current
#   deploy.sh compose <args...>   docker compose with the release's settings; <APP_NAME>-compose wraps it
#
# Nothing here knows the application. The host's settings come from /etc/compose-host/host.env, which Terraform
# writes (APP_NAME, AWS_REGION, DEPLOY_ROOT, DEPLOY_SCRIPT, DEPLOY_ENV, SSM_PREFIX, REGISTRY); the application's
# from <DEPLOY_ENV>.env beside this script (COMPOSE_FILE, COMPOSE_PROFILES and the DEPLOY_* settings below).
#
# The SSM parameters under SSM_PREFIX become, by name:
#   <prefix>/env/NAME          NAME, exported to Compose for interpolation; its value can't hold a newline
#   <prefix>/files/some/path   DEPLOY_ROOT/shared/files/some/path (mode 0600), for Compose to mount
#
# DEPLOY_STOP_FIRST=1         stop the services outside any profile before starting the new release
# DEPLOY_HEALTH_TIMEOUT=300   seconds for every container with a health check to become healthy
# DEPLOY_CHECK_URLS="..."     URLs that must then answer 2xx (TLS not verified: they are this host's own)
# DEPLOY_SYSTEMD_DIR=dir      units installed as <APP_NAME>-<file>, @DEPLOY_ROOT@ and @APP_NAME@ replaced; timers enabled
# DEPLOY_KEEP_RELEASES=5      release directories kept; the current and the previous one always stay
# DEPLOY_IMAGE_PRUNE_AFTER    unused images older than this (a docker filter duration, 168h) are removed
set -Eeuo pipefail
shopt -s nullglob

HOST_ENV=${HOST_ENV:-/etc/compose-host/host.env}

log() { printf '%s %s\n' "$(date -u +%H:%M:%S)" "$*"; }
die() {
	log "error: $*" >&2
	exit 1
}

usage() {
	sed -n '2,6p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//' >&2
}

# Sets script, release_dir, release and shared, exports both settings files, and moves into the release.
load_settings() {
	[[ -r $HOST_ENV ]] || die "no $HOST_ENV: is this a compose-host instance?"
	set -a
	# shellcheck source=/dev/null
	. "$HOST_ENV"
	set +a
	: "${APP_NAME:?}" "${AWS_REGION:?}" "${DEPLOY_ROOT:?}" "${DEPLOY_SCRIPT:?}" "${SSM_PREFIX:?}"

	script=$(readlink -f "${BASH_SOURCE[0]}")
	release_dir=${script%/"$DEPLOY_SCRIPT"}
	[[ $release_dir != "$script" ]] || die "$script is not at $DEPLOY_SCRIPT inside a release"
	release=$(basename "$release_dir")
	shared=$DEPLOY_ROOT/shared

	local app_env
	app_env=$(dirname "$script")/${DEPLOY_ENV:-prod}.env
	[[ -r $app_env ]] || die "no $app_env"
	set -a
	# shellcheck source=/dev/null
	. "$app_env"
	set +a

	export IMAGE_TAG=${IMAGE_TAG:-$release} DEPLOY_SHARED_DIR=$shared RELEASE=$release
	cd "$release_dir"
}

load_secrets() {
	[[ -r $shared/secrets.env ]] || die "no $shared/secrets.env yet: deploy a release first"
	set -a
	# shellcheck source=/dev/null
	. "$shared/secrets.env"
	set +a
}

# Rewrites shared/secrets.env and shared/files from SSM, each swapped in whole.
fetch_parameters() {
	local json name value64 rel key value escaped path env_tmp files_tmp count=0 q="'\''"
	install -d -m 0700 "$shared"
	rm -rf "$shared"/.secrets.env.* "$shared"/.files.*
	json=$(aws ssm get-parameters-by-path --region "$AWS_REGION" --path "$SSM_PREFIX" \
		--recursive --with-decryption --output json)
	env_tmp=$(mktemp "$shared/.secrets.env.XXXXXX")
	files_tmp=$(mktemp -d "$shared/.files.XXXXXX")

	while IFS=$'\t' read -r name value64; do
		rel=${name#"$SSM_PREFIX"/}
		case $rel in
		env/*)
			key=${rel#env/}
			[[ $key =~ ^[A-Za-z_][A-Za-z0-9_]*$ ]] || die "SSM parameter $name: $key is not a variable name"
			value=$(base64 -d <<<"$value64")
			[[ $value != *$'\n'* ]] || die "SSM parameter $name: a variable can't hold a newline"
			escaped=${value//\'/$q}
			printf "%s='%s'\n" "$key" "$escaped" >>"$env_tmp"
			;;
		files/*)
			path=${rel#files/}
			[[ $path != /* && $path != *..* ]] || die "SSM parameter $name: $path is not a relative path"
			install -d -m 0700 "$files_tmp/$(dirname "$path")"
			base64 -d <<<"$value64" >"$files_tmp/$path"
			chmod 0600 "$files_tmp/$path"
			;;
		*)
			log "ignoring SSM parameter $name: neither env/ nor files/"
			continue
			;;
		esac
		count=$((count + 1))
	done < <(jq -r '.Parameters[] | [.Name, (.Value | @base64)] | @tsv' <<<"$json")

	((count > 0)) || die "no parameters under $SSM_PREFIX"
	chmod 0600 "$env_tmp"
	mv -f "$env_tmp" "$shared/secrets.env"
	rm -rf "$shared/files.old"
	if [[ -e $shared/files ]]; then mv "$shared/files" "$shared/files.old"; fi
	mv "$files_tmp" "$shared/files"
	rm -rf "$shared/files.old"
	log "wrote $count parameters from $SSM_PREFIX"
}

registry_login() {
	[[ ${REGISTRY:-} == *.dkr.ecr.*.amazonaws.com ]] || return 0
	aws ecr get-login-password --region "$AWS_REGION" |
		docker login --username AWS --password-stdin "$REGISTRY" >/dev/null 2>&1
	log "logged in to $REGISTRY"
}

install_units() {
	local dir=${DEPLOY_SYSTEMD_DIR:-} unit name changed=0
	[[ -n $dir && -d $dir ]] || return 0
	for unit in "$dir"/*.service "$dir"/*.timer; do
		name=$APP_NAME-$(basename "$unit")
		sed -e "s|@DEPLOY_ROOT@|$DEPLOY_ROOT|g" -e "s|@APP_NAME@|$APP_NAME|g" "$unit" >"/etc/systemd/system/$name.tmp"
		if cmp -s "/etc/systemd/system/$name.tmp" "/etc/systemd/system/$name"; then
			rm -f "/etc/systemd/system/$name.tmp"
		else
			mv -f "/etc/systemd/system/$name.tmp" "/etc/systemd/system/$name"
			changed=1
		fi
	done
	if ((changed)); then systemctl daemon-reload; fi
	for unit in "$dir"/*.timer; do
		systemctl enable --now --quiet "$APP_NAME-$(basename "$unit")"
	done
}

# Waits until no container is starting, failing at once on one unhealthy, dead or exited with an error.
wait_healthy() {
	local deadline=$((SECONDS + ${DEPLOY_HEALTH_TIMEOUT:-300})) states bad pending
	while :; do
		states=$(docker compose ps -a --format '{{.Service}}|{{.State}}|{{.Health}}|{{.ExitCode}}')
		bad=$(awk -F'|' '$3 == "unhealthy" || $2 == "dead" || ($2 == "exited" && $4 != 0)' <<<"$states")
		pending=$(awk -F'|' '$3 == "starting" || $2 == "created" || $2 == "restarting"' <<<"$states")
		if [[ -n $bad ]]; then
			log "failed containers:"$'\n'"$bad"
			return 1
		fi
		if [[ -z $pending ]]; then
			log "every container is up"
			return 0
		fi
		if ((SECONDS >= deadline)); then
			log "still not up after ${DEPLOY_HEALTH_TIMEOUT:-300}s:"$'\n'"$pending"
			return 1
		fi
		sleep 3
	done
}

check_urls() {
	local url attempt
	for url in ${DEPLOY_CHECK_URLS:-}; do
		for attempt in 1 2 3 4 5 6 7 8 9 10; do
			if curl -fsSk --max-time 10 -o /dev/null "$url"; then
				log "ok: $url"
				continue 2
			fi
			sleep 2
		done
		log "no 2xx from $url after $attempt attempts"
		return 1
	done
}

write_wrapper() {
	local wrapper=/usr/local/bin/$APP_NAME-compose
	cat >"$wrapper.tmp" <<EOF
#!/usr/bin/env bash
# docker compose for the release in $DEPLOY_ROOT/current with its settings and secrets; written by deploy.sh.
exec "$DEPLOY_ROOT/current/$DEPLOY_SCRIPT" compose "\$@"
EOF
	chmod 0755 "$wrapper.tmp"
	mv -f "$wrapper.tmp" "$wrapper"
}

prune() {
	local keep=${DEPLOY_KEEP_RELEASES:-5} dir
	while IFS= read -r dir; do
		[[ $dir == "$release_dir" || $(basename "$dir") == "${previous:-}" ]] && continue
		rm -rf -- "$dir"
		log "removed old release $(basename "$dir")"
	done < <(find "$DEPLOY_ROOT/releases" -mindepth 1 -maxdepth 1 -type d ! -name '.*' -printf '%T@ %p\n' |
		sort -rn | tail -n +"$((keep + 1))" | cut -d' ' -f2-)
	docker image prune -af --filter "until=${DEPLOY_IMAGE_PRUNE_AFTER:-168h}" >/dev/null || true
}

report_failure() {
	trap - ERR
	log "the deploy of $release failed; the containers now:"
	docker compose ps -a || true
	log "their last log lines:"
	docker compose logs --no-color --tail=25 || true
	if [[ -n ${previous:-} ]]; then
		log "to roll back: compose-release $previous (safe only if this release's migrations kept the old code working)"
	fi
}

cmd_deploy() {
	[[ ${1:-} == "$release" ]] || die "asked to deploy ${1:-nothing}, but this script belongs to release $release"

	exec 9>"$DEPLOY_ROOT/.deploy.lock"
	flock -w 600 9 || die "another deploy has held the lock for 10 minutes"
	install -d "$DEPLOY_ROOT/logs"
	exec > >(tee -a "$DEPLOY_ROOT/logs/$(date -u +%Y%m%dT%H%M%SZ)-$release.log") 2>&1

	previous=
	if [[ -L $DEPLOY_ROOT/current ]]; then previous=$(basename "$(readlink "$DEPLOY_ROOT/current")"); fi
	trap report_failure ERR
	log "deploying $APP_NAME $release, images tagged $IMAGE_TAG; the running release: ${previous:-none}"

	fetch_parameters
	load_secrets
	registry_login
	docker compose config --quiet
	log "pulling every image before anything stops"
	docker compose pull --quiet
	install_units

	if [[ ${DEPLOY_STOP_FIRST:-0} == 1 ]]; then
		log "stopping the services outside any profile"
		COMPOSE_PROFILES='' docker compose stop
	fi
	log "starting $release"
	docker compose up -d --no-build --pull never --remove-orphans
	wait_healthy
	check_urls

	touch "$release_dir"
	ln -sfn "$release_dir" "$DEPLOY_ROOT/.current.tmp"
	mv -Tf "$DEPLOY_ROOT/.current.tmp" "$DEPLOY_ROOT/current"
	write_wrapper
	prune
	trap - ERR
	log "deployed $release"
}

main() {
	local cmd=${1:-}
	shift || true
	case $cmd in
	deploy)
		load_settings
		cmd_deploy "$@"
		;;
	compose)
		load_settings
		load_secrets
		exec docker compose "$@"
		;;
	*)
		usage
		exit 2
		;;
	esac
}

main "$@"

#!/usr/bin/env bash
# compose-release <release>: fetches a release bundle from the artifact bucket and runs the deploy script inside it.
# Installed at /usr/local/bin/compose-release by the instance's user data; CI calls it through SSM Run Command.
# The bundle is s3://ARTIFACT_BUCKET/RELEASE_PREFIX/<release>.tar.gz; its DEPLOY_SCRIPT does the rest, so the
# deploy logic always matches the release being deployed and this script never has to change.
set -euo pipefail
export HOME=${HOME:-/root} # SSM Run Command may start without one; docker and the AWS CLI keep config there

release=${1:?usage: compose-release <release>}
[[ $release =~ ^[A-Za-z0-9][A-Za-z0-9._-]*$ ]] || {
	echo "not a release name: $release" >&2
	exit 2
}

set -a
# shellcheck source=/dev/null
. /etc/compose-host/host.env
set +a
: "${DEPLOY_ROOT:?}" "${ARTIFACT_BUCKET:?}" "${DEPLOY_SCRIPT:?}" "${AWS_REGION:?}"

releases=$DEPLOY_ROOT/releases
dir=$releases/$release
install -d -m 0755 "$releases"
if [[ ! -d $dir ]]; then
	tmp=$(mktemp -d "$releases/.incoming.XXXXXX")
	trap 'rm -rf "$tmp"' EXIT
	aws s3 cp --only-show-errors --region "$AWS_REGION" \
		"s3://$ARTIFACT_BUCKET/${RELEASE_PREFIX:-releases}/$release.tar.gz" - | tar -xz -C "$tmp"
	chmod 0755 "$tmp"
	mv "$tmp" "$dir"
	trap - EXIT
fi

exec "$dir/$DEPLOY_SCRIPT" deploy "$release"

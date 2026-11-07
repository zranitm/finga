#!/usr/bin/env bash
# Copies a Terraform root's github_variables output into a repository's GitHub Actions variables, with gh.
#   infra/terraform/gh-variables.sh <terraform root> [owner/repo]
# Without owner/repo, gh uses the repository of the current directory.
set -euo pipefail

dir=${1:?usage: gh-variables.sh <terraform root> [owner/repo]}
args=()
if [[ -n ${2:-} ]]; then args=(--repo "$2"); fi

terraform -chdir="$dir" output -json github_variables |
	jq -r 'to_entries[] | [.key, .value] | @tsv' |
	while IFS=$'\t' read -r key value; do
		gh variable set "$key" --body "$value" ${args[@]+"${args[@]}"} # the guard keeps bash 3.2 (macOS) happy when empty
		echo "set $key"
	done

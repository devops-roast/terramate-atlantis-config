#!/usr/bin/env bash
# Runs E2E scenarios against the Gitea + Atlantis environment.
#
# Usage: run-test.sh [SCENARIO...]
#   SCENARIO is a file name in e2e/scenarios/ with or without the .sh suffix
#   (e.g. 01-plan-apply). With no arguments every scenario runs in order.
#
# On failure the Atlantis and Gitea logs are dumped and the exit code is 1.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
E2E_DIR="$(dirname "$SCRIPT_DIR")"
SCENARIO_DIR="${E2E_DIR}/scenarios"

command -v jq >/dev/null || {
	echo "ERROR: jq is required"
	exit 1
}

if [ ! -f "$E2E_DIR/.env.secrets" ]; then
	echo "ERROR: .env.secrets not found — run 'make setup' first"
	exit 1
fi

# shellcheck source=/dev/null
source "$E2E_DIR/.env.secrets"
export E2E_GITEA_TOKEN="${ATLANTIS_GITEA_TOKEN:?ATLANTIS_GITEA_TOKEN not set}"
export E2E_DEV_TOKEN="${E2E_DEV_TOKEN:?E2E_DEV_TOKEN not set — re-run 'make setup'}"

E2E_WORK_DIR=$(mktemp -d)
export E2E_WORK_DIR
trap 'rm -rf "$E2E_WORK_DIR"' EXIT

scenarios=()
if [ "$#" -gt 0 ]; then
	for name in "$@"; do
		file="${SCENARIO_DIR}/${name%.sh}.sh"
		[ -f "$file" ] || {
			echo "ERROR: unknown scenario $name"
			exit 1
		}
		scenarios+=("$file")
	done
else
	for file in "$SCENARIO_DIR"/*.sh; do
		scenarios+=("$file")
	done
fi

dump_logs() {
	echo ""
	echo "=== Atlantis logs (last 200 lines) ==="
	docker logs --tail 200 "${ATLANTIS_CONTAINER:-e2e-atlantis}" 2>&1 || true
	echo ""
	echo "=== Gitea logs (last 50 lines) ==="
	docker logs --tail 50 "${GITEA_CONTAINER:-e2e-gitea}" 2>&1 || true
}

echo "=== E2E scenarios ==="
passed=()
failed=()
for file in "${scenarios[@]}"; do
	scenario="$(basename "$file" .sh)"
	echo ""
	echo "▶ ${scenario}"
	start=$SECONDS
	# Each scenario runs in its own subshell so a failed command or assertion
	# stops only that scenario, and its pull request is always closed. The
	# subshell must not be an `if` condition, or bash would disable `set -e`
	# inside it.
	set +e
	(
		set -euo pipefail
		# shellcheck source=e2e/scripts/lib.sh
		source "$SCRIPT_DIR/lib.sh"
		# shellcheck source=/dev/null
		source "$file"
		trap close_pr EXIT
		scenario_run
	)
	rc=$?
	set -e
	if [ "$rc" -eq 0 ]; then
		passed+=("$scenario")
		echo "  PASS ${scenario} ($((SECONDS - start))s)"
	else
		failed+=("$scenario")
		echo "  FAIL ${scenario} ($((SECONDS - start))s)"
	fi
done

echo ""
echo "=== Summary: ${#passed[@]} passed, ${#failed[@]} failed ==="
if [ "${#failed[@]}" -gt 0 ]; then
	printf '  failed: %s\n' "${failed[@]}"
	dump_logs
	exit 1
fi

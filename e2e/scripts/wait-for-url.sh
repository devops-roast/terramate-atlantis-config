#!/usr/bin/env bash
set -euo pipefail

URL="${1:?Usage: wait-for-url.sh URL [TIMEOUT_SECONDS]}"
TIMEOUT="${2:-120}"
INTERVAL=3
ELAPSED=0

printf "Waiting for %s " "$URL"

while [ "$ELAPSED" -lt "$TIMEOUT" ]; do
	if curl -sf "$URL" >/dev/null 2>&1; then
		printf "\n✓ %s is ready (%ds)\n" "$URL" "$ELAPSED"
		exit 0
	fi
	printf "."
	sleep "$INTERVAL"
	ELAPSED=$((ELAPSED + INTERVAL))
done

printf "\n✗ Timeout after %ds waiting for %s\n" "$TIMEOUT" "$URL"
exit 1

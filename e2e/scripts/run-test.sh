#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
E2E_DIR="$(dirname "$SCRIPT_DIR")"

if [ ! -f "$E2E_DIR/.env.secrets" ]; then
	echo "ERROR: .env.secrets not found — run 'make setup' first"
	exit 1
fi

# shellcheck source=/dev/null
source "$E2E_DIR/.env.secrets"
TOKEN="${ATLANTIS_GITEA_TOKEN:?ATLANTIS_GITEA_TOKEN not set}"

GITEA_URL="${GITEA_URL:-http://localhost:3000}"
GITEA_ADMIN_USER="${GITEA_ADMIN_USER:-e2e-admin}"
GITEA_ADMIN_PASS="${GITEA_ADMIN_PASS:-e2e-password-123}"
TEST_REPO="${TEST_REPO:-e2e-test-repo}"
ATLANTIS_TIMEOUT="${ATLANTIS_TIMEOUT:-120}"

BRANCH_NAME="test/e2e-$(date +%s)"

echo "=== E2E Test ==="

echo "[1/5] Cloning repository …"
WORK_DIR=$(mktemp -d)
trap 'rm -rf "$WORK_DIR"' EXIT

git clone \
	"http://${GITEA_ADMIN_USER}:${GITEA_ADMIN_PASS}@localhost:3000/${GITEA_ADMIN_USER}/${TEST_REPO}.git" \
	"$WORK_DIR/repo" 2>/dev/null
cd "$WORK_DIR/repo"

echo "[2/5] Creating branch ${BRANCH_NAME} with a new stack …"
git checkout -b "$BRANCH_NAME" 2>/dev/null

mkdir -p stacks/staging
cat >stacks/staging/stack.tm.hcl <<'HCL'
stack {
  name = "staging"
  id   = "e2e00000-0000-0000-0000-000000000099"
}
HCL

cat >stacks/staging/main.tf <<'TF'
terraform {
  required_version = ">= 1.4"
}

resource "terraform_data" "staging" {
  input = "e2e-staging"
}
TF

git add -A
git \
	-c user.name="E2E Bot" \
	-c user.email="e2e@test.local" \
	commit -m "Add staging stack" >/dev/null
git push origin "$BRANCH_NAME" 2>/dev/null
echo "      branch pushed"

echo "[3/5] Opening pull request …"
PR_RESPONSE=$(curl -sf -X POST \
	"${GITEA_URL}/api/v1/repos/${GITEA_ADMIN_USER}/${TEST_REPO}/pulls" \
	-H "Content-Type: application/json" \
	-H "Authorization: token ${TOKEN}" \
	-d "{
    \"title\":\"E2E: add staging stack\",
    \"body\":\"Automated E2E test — adds a third Terramate stack.\",
    \"head\":\"${BRANCH_NAME}\",
    \"base\":\"main\"
  }")

PR_NUMBER=$(echo "$PR_RESPONSE" | grep -o '"number":[0-9]*' | head -1 | cut -d: -f2)
if [ -z "$PR_NUMBER" ]; then
	echo "ERROR: could not create pull request"
	echo "$PR_RESPONSE"
	exit 1
fi
echo "      PR #${PR_NUMBER} — ${GITEA_URL}/${GITEA_ADMIN_USER}/${TEST_REPO}/pulls/${PR_NUMBER}"

echo "[4/5] Waiting for Atlantis to comment (timeout ${ATLANTIS_TIMEOUT}s) …"
ELAPSED=0
INTERVAL=5
FOUND=false

while [ "$ELAPSED" -lt "$ATLANTIS_TIMEOUT" ]; do
	COMMENTS=$(curl -sf \
		"${GITEA_URL}/api/v1/repos/${GITEA_ADMIN_USER}/${TEST_REPO}/issues/${PR_NUMBER}/comments" \
		-H "Authorization: token ${TOKEN}" 2>/dev/null || echo "[]")

	if echo "$COMMENTS" | grep -qiE 'plan|atlantis|terraform'; then
		FOUND=true
		break
	fi

	sleep "$INTERVAL"
	ELAPSED=$((ELAPSED + INTERVAL))
	printf "\r      %ds / %ds" "$ELAPSED" "$ATLANTIS_TIMEOUT"
done
echo ""

echo "[5/5] Results"
echo "---"

if [ "$FOUND" = true ]; then
	echo "✓ PASS — Atlantis posted a comment on PR #${PR_NUMBER}"
	echo ""
	echo "Comment excerpt:"
	echo "$COMMENTS" | jq -r '.[].body' 2>/dev/null | head -40 || echo "$COMMENTS"
	EXIT_CODE=0
else
	echo "✗ FAIL — Atlantis did not comment within ${ATLANTIS_TIMEOUT}s"
	echo ""
	echo "Troubleshooting:"
	echo "  make logs-atlantis   — check Atlantis logs for errors"
	echo "  make logs-gitea      — check Gitea webhook delivery"
	echo "  Visit ${GITEA_URL}/${GITEA_ADMIN_USER}/${TEST_REPO}/settings/hooks to inspect deliveries"
	EXIT_CODE=1
fi

echo "---"
exit "$EXIT_CODE"

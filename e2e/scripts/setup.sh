#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
E2E_DIR="$(dirname "$SCRIPT_DIR")"

GITEA_URL="${GITEA_URL:-http://localhost:3000}"
GITEA_ADMIN_USER="${GITEA_ADMIN_USER:-e2e-admin}"
GITEA_ADMIN_PASS="${GITEA_ADMIN_PASS:-e2e-password-123}"
GITEA_ADMIN_EMAIL="${GITEA_ADMIN_EMAIL:-admin@e2e.local}"
TEST_REPO="${TEST_REPO:-e2e-test-repo}"
WEBHOOK_SECRET="${WEBHOOK_SECRET:-e2e-webhook-secret}"
GITEA_CONTAINER="${GITEA_CONTAINER:-e2e-gitea}"
E2E_DEV_USER="${E2E_DEV_USER:-e2e-dev}"

echo "=== Gitea setup ==="

echo "[1/6] Creating admin user …"
docker exec --user 1000 "$GITEA_CONTAINER" gitea admin user create \
	--username "$GITEA_ADMIN_USER" \
	--password "$GITEA_ADMIN_PASS" \
	--email "$GITEA_ADMIN_EMAIL" \
	--admin \
	--must-change-password=false 2>/dev/null &&
	echo "      user created" ||
	echo "      user already exists"

echo "[2/6] Generating API token …"
TOKEN=$(docker exec --user 1000 "$GITEA_CONTAINER" gitea admin user generate-access-token \
	--username "$GITEA_ADMIN_USER" \
	--token-name "e2e-$(date +%s)" \
	--scopes "write:user,write:repository,write:issue,write:organization,read:user,read:repository,read:issue,read:organization" \
	--raw 2>/dev/null)

if [ -z "$TOKEN" ]; then
	echo "ERROR: failed to generate Gitea API token"
	exit 1
fi
echo "      token: ${TOKEN:0:8}…"

echo "[3/6] Creating developer user ${E2E_DEV_USER} …"
# Atlantis ignores comments written by its own account, so scenarios comment
# (e.g. "atlantis apply") as a separate user.
docker exec --user 1000 "$GITEA_CONTAINER" gitea admin user create \
	--username "$E2E_DEV_USER" \
	--password "$GITEA_ADMIN_PASS" \
	--email "${E2E_DEV_USER}@e2e.local" \
	--must-change-password=false 2>/dev/null &&
	echo "      user created" ||
	echo "      user already exists"
DEV_TOKEN=$(docker exec --user 1000 "$GITEA_CONTAINER" gitea admin user generate-access-token \
	--username "$E2E_DEV_USER" \
	--token-name "e2e-$(date +%s)" \
	--scopes "write:issue,read:repository,read:user" \
	--raw 2>/dev/null)
if [ -z "$DEV_TOKEN" ]; then
	echo "ERROR: failed to generate developer API token"
	exit 1
fi

cat >"$E2E_DIR/.env.secrets" <<EOF
ATLANTIS_GITEA_TOKEN=${TOKEN}
E2E_DEV_TOKEN=${DEV_TOKEN}
EOF
echo "      tokens written to .env.secrets"

echo "[4/6] Creating repository ${TEST_REPO} …"
HTTP_CODE=$(curl -s -o /dev/null -w "%{http_code}" -X POST \
	"${GITEA_URL}/api/v1/user/repos" \
	-H "Content-Type: application/json" \
	-H "Authorization: token ${TOKEN}" \
	-d "{
    \"name\":\"${TEST_REPO}\",
    \"description\":\"E2E test repository for terramate-atlantis-config\",
    \"private\":false,
    \"auto_init\":true,
    \"default_branch\":\"main\"
  }")
case "$HTTP_CODE" in
201) echo "      repository created" ;;
409) echo "      repository already exists" ;;
*)
	echo "ERROR: unexpected HTTP ${HTTP_CODE} creating repository"
	exit 1
	;;
esac

echo "[5/6] Pushing fixture files …"
WORK_DIR=$(mktemp -d)
trap 'rm -rf "$WORK_DIR"' EXIT

# Same URL as GITEA_URL (which may use another host port), with credentials.
git clone \
	"${GITEA_URL/:\/\//://${GITEA_ADMIN_USER}:${GITEA_ADMIN_PASS}@}/${GITEA_ADMIN_USER}/${TEST_REPO}.git" \
	"$WORK_DIR/repo" 2>/dev/null

cp -r "$E2E_DIR/fixtures/"* "$WORK_DIR/repo/"

(
	cd "$WORK_DIR/repo"
	git add -A
	if git diff --cached --quiet; then
		echo "      fixtures already present"
	else
		git \
			-c user.name="E2E Bot" \
			-c user.email="e2e@test.local" \
			commit -m "Add Terramate project fixtures" >/dev/null
		git push origin main 2>/dev/null
		echo "      fixtures pushed to main"
	fi
)

echo "[6/6] Creating webhook → Atlantis …"
# Re-running setup must not add a second webhook, or Atlantis would receive
# every event twice. Only "pull_request_only" (opened/closed/...) and
# "pull_request_comment" (Gitea does not send "issue_comment" for comments on
# pull requests) are subscribed: Gitea also sends a sync event when a pull
# request is opened,
# which Atlantis handles as a second "opened" event and races the first
# autoplan. Scenarios that push more commits comment "atlantis plan" instead.
if curl -sf "${GITEA_URL}/api/v1/repos/${GITEA_ADMIN_USER}/${TEST_REPO}/hooks" \
	-H "Authorization: token ${TOKEN}" | grep -q 'http://atlantis:4141/events'; then
	echo "      webhook already exists"
	HTTP_CODE=201
else
	HTTP_CODE=$(curl -s -o /dev/null -w "%{http_code}" -X POST \
	"${GITEA_URL}/api/v1/repos/${GITEA_ADMIN_USER}/${TEST_REPO}/hooks" \
	-H "Content-Type: application/json" \
	-H "Authorization: token ${TOKEN}" \
	-d "{
    \"type\":\"gitea\",
    \"config\":{
      \"url\":\"http://atlantis:4141/events\",
      \"content_type\":\"json\",
      \"secret\":\"${WEBHOOK_SECRET}\"
    },
    \"events\":[\"pull_request_only\",\"pull_request_comment\"],
    \"active\":true
  }")
	[ "$HTTP_CODE" != 201 ] || echo "      webhook created"
fi
case "$HTTP_CODE" in
201) ;;
*)
	echo "ERROR: unexpected HTTP ${HTTP_CODE} creating webhook"
	exit 1
	;;
esac

echo ""
echo "=== Setup complete ==="
echo "  Gitea UI : ${GITEA_URL}"
echo "  Repo     : ${GITEA_URL}/${GITEA_ADMIN_USER}/${TEST_REPO}"
echo "  Atlantis : ${ATLANTIS_URL:-http://localhost:4141}"

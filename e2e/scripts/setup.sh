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

echo "[3/6] Persisting token …"
cat >"$E2E_DIR/.env.secrets" <<EOF
ATLANTIS_GITEA_TOKEN=${TOKEN}
EOF
echo "      written to .env.secrets"

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
*) echo "      WARNING: unexpected HTTP ${HTTP_CODE}" ;;
esac

echo "[5/6] Pushing fixture files …"
WORK_DIR=$(mktemp -d)
trap 'rm -rf "$WORK_DIR"' EXIT

git clone \
	"http://${GITEA_ADMIN_USER}:${GITEA_ADMIN_PASS}@localhost:3000/${GITEA_ADMIN_USER}/${TEST_REPO}.git" \
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
    \"events\":[\"pull_request\",\"pull_request_comment\",\"issue_comment\"],
    \"active\":true
  }")
case "$HTTP_CODE" in
201) echo "      webhook created" ;;
*) echo "      webhook already exists or HTTP ${HTTP_CODE}" ;;
esac

echo ""
echo "=== Setup complete ==="
echo "  Gitea UI : ${GITEA_URL}"
echo "  Repo     : ${GITEA_URL}/${GITEA_ADMIN_USER}/${TEST_REPO}"
echo "  Atlantis : http://localhost:4141"

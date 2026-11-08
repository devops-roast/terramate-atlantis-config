#!/usr/bin/env bash
# Helpers shared by the E2E runner and scenarios.
#
# A scenario is a file in e2e/scenarios/ that defines `scenario_run`. Inside it,
# use the helpers below: open a branch, change stacks, open a pull request,
# wait for Atlantis comments and assert on them. Any failed assertion aborts the
# scenario with a non-zero exit code.

: "${GITEA_URL:=http://localhost:3000}"
: "${GITEA_ADMIN_USER:=e2e-admin}"
: "${GITEA_ADMIN_PASS:=e2e-password-123}"
: "${TEST_REPO:=e2e-test-repo}"
: "${ATLANTIS_TIMEOUT:=180}"

E2E_REPO_API="${GITEA_URL}/api/v1/repos/${GITEA_ADMIN_USER}/${TEST_REPO}"

# State of the current scenario.
REPO_DIR=""
BRANCH=""
PR_NUMBER=""
LAST_COMMENT_ID=0
LAST_COMMENT=""
OWN_COMMENT_IDS=" "

log() { printf '      %s\n' "$*"; }

fail() {
	printf '      ✗ %s\n' "$*" >&2
	exit 1
}

pass() { printf '      ✓ %s\n' "$*"; }

# gitea_api METHOD PATH [JSON]: call the Gitea API for the test repository as
# the admin (the account Atlantis also uses), or as E2E_API_TOKEN if set.
gitea_api() {
	local method="$1" path="$2" data="${3:-}"
	local token="${E2E_API_TOKEN:-${E2E_GITEA_TOKEN:?}}"
	local args=(-sS --fail-with-body -X "$method" -H "Authorization: token ${token}" -H "Content-Type: application/json")
	if [ -n "$data" ]; then
		args+=(-d "$data")
	fi
	curl "${args[@]}" "${E2E_REPO_API}${path}"
}

# new_branch NAME: clone the test repo into a fresh directory, check out a new
# branch and cd into it. Sets REPO_DIR and BRANCH.
new_branch() {
	local name="$1"
	local clone_url="${GITEA_URL/:\/\//://${GITEA_ADMIN_USER}:${GITEA_ADMIN_PASS}@}/${GITEA_ADMIN_USER}/${TEST_REPO}.git"

	BRANCH="e2e/${name}-$(date +%s)"
	REPO_DIR="${E2E_WORK_DIR:?}/${name}"
	rm -rf "$REPO_DIR"
	git clone -q "$clone_url" "$REPO_DIR"
	cd "$REPO_DIR" || fail "cannot cd to $REPO_DIR"
	git checkout -q -b "$BRANCH"
	log "branch ${BRANCH}"
}

# add_stack DIR NAME [EXTRA_HCL]: create a stack with a providerless resource.
add_stack() {
	local dir="$1" name="$2" extra="${3:-}"
	mkdir -p "$dir"
	cat >"${dir}/stack.tm.hcl" <<HCL
stack {
  name = "${name}"
}
${extra}
HCL
	cat >"${dir}/main.tf" <<TF
terraform {
  required_version = ">= 1.4"
}

resource "terraform_data" "this" {
  input = "e2e-${name}"
}
TF
}

# touch_stack DIR: change a stack's terraform so Atlantis autoplans it.
touch_stack() {
	printf '\n# changed by e2e at %s\n' "$(date +%s)-${RANDOM}" >>"$1/main.tf"
}

# open_pr TITLE: commit everything, push the branch and open a pull request.
# Sets PR_NUMBER.
open_pr() {
	local title="$1" response
	git add -A
	git -c user.name="E2E Bot" -c user.email="e2e@test.local" commit -q -m "$title"
	git push -q origin "$BRANCH" 2>/dev/null

	response=$(gitea_api POST /pulls "$(jq -n --arg t "$title" --arg h "$BRANCH" \
		'{title: $t, head: $h, base: "main", body: "Automated E2E scenario."}')")
	PR_NUMBER=$(jq -r '.number' <<<"$response")
	[ -n "$PR_NUMBER" ] && [ "$PR_NUMBER" != "null" ] || fail "could not open pull request: $response"
	LAST_COMMENT_ID=0
	OWN_COMMENT_IDS=" "
	log "PR #${PR_NUMBER} ${GITEA_URL}/${GITEA_ADMIN_USER}/${TEST_REPO}/pulls/${PR_NUMBER}"
}

# comment BODY: post a comment (e.g. "atlantis apply") on the pull request.
# It is written by the developer account, because Atlantis ignores comments
# from its own account.
comment() {
	local response id
	response=$(E2E_API_TOKEN="${E2E_DEV_TOKEN:?}" gitea_api POST "/issues/${PR_NUMBER}/comments" \
		"$(jq -n --arg b "$1" '{body: $b}')")
	id=$(jq -r '.id' <<<"$response")
	OWN_COMMENT_IDS="${OWN_COMMENT_IDS}${id} "
	log "commented: $1"
}

# wait_for_comment REGEX [TIMEOUT]: wait for a new comment not written by the
# scenario whose body matches the extended REGEX. Sets LAST_COMMENT.
# Atlantis uses the same Gitea account as the scenario, so comments are told
# apart by id rather than author.
wait_for_comment() {
	local regex="$1" timeout="${2:-$ATLANTIS_TIMEOUT}" elapsed=0 comments ids id body
	while [ "$elapsed" -lt "$timeout" ]; do
		comments=$(gitea_api GET "/issues/${PR_NUMBER}/comments" 2>/dev/null || echo '[]')
		ids=$(jq -r --argjson after "$LAST_COMMENT_ID" '.[] | select(.id > $after) | .id' <<<"$comments")
		for id in $ids; do
			case "$OWN_COMMENT_IDS" in *" ${id} "*) continue ;; esac
			body=$(jq -r --argjson id "$id" '.[] | select(.id == $id) | .body' <<<"$comments")
			if grep -qE "$regex" <<<"$body"; then
				LAST_COMMENT_ID="$id"
				LAST_COMMENT="$body"
				log "Atlantis replied after ${elapsed}s"
				return 0
			fi
		done
		sleep 5
		elapsed=$((elapsed + 5))
	done
	printf '      comments so far:\n' >&2
	jq -r '.[] | "--- #\(.id)\n\(.body)"' <<<"$comments" >&2 || true
	fail "no comment matching /${regex}/ within ${timeout}s"
}

# assert_comment_contains TEXT...: every TEXT must appear in LAST_COMMENT.
assert_comment_contains() {
	local text
	for text in "$@"; do
		grep -qF -- "$text" <<<"$LAST_COMMENT" || {
			printf '%s\n' "$LAST_COMMENT" >&2
			fail "comment does not contain: $text"
		}
	done
	pass "comment contains: $*"
}

# assert_comment_not_contains TEXT...: no TEXT may appear in LAST_COMMENT.
assert_comment_not_contains() {
	local text
	for text in "$@"; do
		if grep -qF -- "$text" <<<"$LAST_COMMENT"; then
			printf '%s\n' "$LAST_COMMENT" >&2
			fail "comment unexpectedly contains: $text"
		fi
	done
	pass "comment does not contain: $*"
}

# assert_project PROJECT DIR: LAST_COMMENT lists the Atlantis project with the
# given name and directory.
assert_project() {
	assert_comment_contains "project: \`$1\` dir: \`$2\`"
}

# assert_no_project DIR: LAST_COMMENT does not mention the directory.
assert_no_project() {
	assert_comment_not_contains "dir: \`$1\`"
}

# assert_no_errors: LAST_COMMENT reports no failed plan/apply.
assert_no_errors() {
	assert_comment_not_contains "**Plan Error**" "**Plan Failed**" "**Apply Error**" "**Apply Failed**" "Error: "
}

# close_pr: release the pull request's Atlantis locks and close it. Atlantis
# does not treat Gitea's "closed" event as a close, so the locks are released
# explicitly with `atlantis unlock`; otherwise later scenarios touching the same
# stacks would be blocked.
close_pr() {
	[ -n "$PR_NUMBER" ] || return 0
	(
		comment "atlantis unlock"
		wait_for_comment 'unlocked|[Ll]ock' 60
	) >/dev/null 2>&1 || log "WARNING: could not confirm unlock of PR #${PR_NUMBER}"
	gitea_api PATCH "/pulls/${PR_NUMBER}" '{"state":"closed"}' >/dev/null || true
	log "closed PR #${PR_NUMBER}"
}

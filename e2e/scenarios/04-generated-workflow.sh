# shellcheck shell=bash
# --generate-workflows: projects run Terraform through `terramate run` inside
# Atlantis.
#
# - The pull request branch is behind main, which makes Terramate's git
#   safeguards refuse to run. With every safeguard kept the plan fails; with
#   the generated defaults (--disable-safeguards=git) it succeeds.
# - A parent stack contains a nested child stack. Each is its own project, and
#   the parent's project must not also run the child (--no-recursive): each
#   resource is planned exactly once across the whole pull request.

scenario_run() {
	new_branch generated-workflow
	advance_main # before any change: advance_main commits the working tree

	# The fixture's root config turns Terramate's git safeguards off; restore
	# Terramate's defaults for this pull request, as most projects run them.
	cat >terramate.tm.hcl <<'HCL'
terramate {
  config {}
}
HCL

	add_stack stacks/tm/parent tm-parent
	add_stack stacks/tm/parent/child tm-child

	# Keep every Terramate safeguard first, to show the failure being fixed.
	cat >.terramate-atlantis.yaml <<'YAML'
generate_workflows: true
workflow_disable_safeguards: []
YAML

	open_pr "E2E: generated terramate workflow"

	wait_for_comment 'Ran Plan|Error'
	grep -qE 'out-of-date|untracked files|uncommitted files' <<<"$LAST_COMMENT" || {
		printf '%s\n' "$LAST_COMMENT" >&2
		fail "expected a Terramate git safeguard to refuse the plan"
	}
	local refusal
	# shellcheck disable=SC2016 # the backticks are literal Markdown delimiters
	refusal=$(grep -m1 -oE '[^`]*(out-of-date|untracked files|uncommitted files)[^`]*' <<<"$LAST_COMMENT")
	pass "terramate run refuses the checkout when every safeguard is kept: ${refusal}"

	# Generated defaults: --no-recursive --disable-safeguards=git.
	cat >.terramate-atlantis.yaml <<'YAML'
generate_workflows: true
YAML
	push_commit "E2E: use generated workflow defaults"
	comment "atlantis plan"

	wait_for_comment 'Ran Plan|Error'
	assert_comment_contains "Ran Plan for 2 projects"
	assert_project tm-parent stacks/tm/parent
	assert_project tm-parent-child stacks/tm/parent/child
	assert_comment_contains "Plan: 1 to add, 0 to change, 0 to destroy."
	# With recursion the parent's plan would also contain the child's resource.
	assert_comment_count '"e2e-tm-parent"' 1
	assert_comment_count '"e2e-tm-child"' 1
	assert_no_errors

	assert_generated_contains "$(
		cat <<'YAML'
  - name: tm-parent
    dir: stacks/tm/parent
    workflow: terramate
YAML
	)"
	# shellcheck disable=SC2016 # $PLANFILE is literal text in atlantis.yaml
	assert_generated_contains 'run: terramate run --no-recursive --disable-safeguards=git -- terraform plan -input=false -out="$PLANFILE"'

	comment "atlantis apply"
	wait_for_comment 'Ran Apply|Error'
	assert_comment_contains "Ran Apply for 2 projects" "Apply complete! Resources: 1 added, 0 changed, 0 destroyed."
	assert_project tm-parent stacks/tm/parent
	assert_project tm-parent-child stacks/tm/parent/child
	assert_no_errors
}

# shellcheck shell=bash
# The pre-workflow hook generates atlantis.yaml; Atlantis autoplans exactly the
# changed, non-skipped stacks under the generated project names and applies
# them on `atlantis apply`.

scenario_run() {
	new_branch plan-apply

	add_stack stacks/staging staging
	touch_stack stacks/dev
	touch_stack stacks/skipped # atlantis_skip = true: must not become a project

	open_pr "E2E: plan and apply"

	wait_for_comment 'Ran Plan|Error'
	assert_comment_contains "Ran Plan for 2 projects"
	assert_project dev stacks/dev
	assert_project staging stacks/staging
	assert_no_project stacks/skipped
	assert_no_project stacks/prod
	assert_comment_contains "Plan: 1 to add, 0 to change, 0 to destroy."
	assert_no_errors

	comment "atlantis apply"
	wait_for_comment 'Ran Apply|Error'
	assert_comment_contains "Ran Apply for 2 projects"
	assert_project dev stacks/dev
	assert_project staging stacks/staging
	assert_comment_contains "Apply complete! Resources: 1 added, 0 changed, 0 destroyed."
	assert_no_errors
}

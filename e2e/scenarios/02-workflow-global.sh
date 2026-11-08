# shellcheck shell=bash
# A stack's atlantis_workflow global selects the server-side workflow for its
# generated project.

scenario_run() {
	new_branch workflow-global

	touch_stack stacks/prod # globals { atlantis_workflow = "production" }

	open_pr "E2E: workflow from globals"

	wait_for_comment 'Ran Plan|Error'
	assert_project prod stacks/prod
	assert_comment_contains "e2e-workflow=production"
	assert_no_project stacks/dev
	assert_no_errors
}

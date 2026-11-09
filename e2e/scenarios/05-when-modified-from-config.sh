# shellcheck shell=bash
# --when-modified-from-config: the stacks under stacks/acme don't commit their
# Terraform code (the pre-workflow hook runs `terramate generate`). A pull
# request that changes only their parent's globals file touches no stack
# directory, yet Atlantis autoplans both stacks, and only them, from the
# when_modified entries derived from the Terramate config.

scenario_run() {
	new_branch when-modified-from-config

	cat >.terramate-atlantis.yaml <<'YAML'
when_modified_from_config: true
YAML
	# The only Terramate change: a global inherited by stacks/acme/{api,web}.
	cat >stacks/acme/account.tm.hcl <<'HCL'
# Globals for every stack below. Scenario 05 changes only this file.
globals {
  account_label = "acme-v2"
}
HCL

	open_pr "E2E: when_modified from config"

	wait_for_comment 'Ran Plan|Error'
	assert_comment_contains "Ran Plan for 2 projects"
	assert_project acme-api stacks/acme/api
	assert_project acme-web stacks/acme/web
	assert_no_project stacks/dev
	assert_no_project stacks/prod
	# The plans use the code generated from the changed global.
	assert_comment_contains '"acme-v2-api"' '"acme-v2-web"'
	assert_no_errors

	assert_generated_contains "$(
		cat <<'YAML'
  - name: acme-api
    dir: stacks/acme/api
    autoplan:
      enabled: true
      when_modified:
        - '*.tf'
        - '*.tf.json'
        - .terraform.lock.hcl
        - '*.tm.hcl'
        - '*.tm'
        - ../*.tm.hcl
YAML
	)"

	# Without the flag, the same change matches no project.
	cat >.terramate-atlantis.yaml <<'YAML'
when_modified_from_config: false
YAML
	push_commit "E2E: turn when_modified_from_config off"
	comment "atlantis plan"

	wait_for_comment 'Ran Plan|Error'
	assert_comment_contains "Ran Plan for 0 projects"
	assert_no_project stacks/acme/api
	assert_no_project stacks/acme/web
}

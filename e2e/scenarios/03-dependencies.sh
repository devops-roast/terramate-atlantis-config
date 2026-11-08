# shellcheck shell=bash
# Stack ordering (`after`, including `tag:` entries) becomes Atlantis
# depends_on with project names: Atlantis refuses to apply a project before
# its dependencies and applies the whole pull request in order.

scenario_run() {
	new_branch dependencies

	add_stack stacks/deps/network network
	add_stack stacks/deps/cache cache
	cat >stacks/deps/cache/stack.tm.hcl <<'HCL'
stack {
  name = "cache"
  tags = ["cache"]
}
HCL
	add_stack stacks/deps/app app
	cat >stacks/deps/app/stack.tm.hcl <<'HCL'
stack {
  name  = "app"
  after = ["../network", "tag:cache"]
}
HCL
	cat >.terramate-atlantis.yaml <<'YAML'
depends_on: true
execution_order_groups: true
YAML

	open_pr "E2E: dependencies"

	wait_for_comment 'Ran Plan|Error'
	assert_comment_contains "Ran Plan for 3 projects"
	assert_project deps-network stacks/deps/network
	assert_project deps-cache stacks/deps/cache
	assert_project deps-app stacks/deps/app
	assert_no_errors

	assert_generated_contains "$(
		cat <<'YAML'
  - name: deps-app
    dir: stacks/deps/app
    execution_order_group: 1
    depends_on:
      - deps-cache
      - deps-network
YAML
	)"

	# Atlantis names only the first unapplied dependency.
	comment "atlantis apply -p deps-app"
	wait_for_comment 'Ran Apply|Error'
	assert_comment_contains "Can't apply your project unless you apply its dependencies"
	assert_comment_not_contains "Apply complete!"

	comment "atlantis apply"
	wait_for_comment 'Ran Apply|Error'
	assert_comment_contains "Ran Apply for 3 projects"
	assert_no_errors
}

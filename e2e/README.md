# E2E tests

The suite runs a real Atlantis server against a local Gitea instance. Atlantis
calls `terramate-atlantis-config generate` as a pre-workflow hook (see
`repos.yaml`), so every scenario exercises the whole path: stack discovery,
generated `atlantis.yaml`, Atlantis autoplan and apply.

```sh
make e2e                               # build, start Gitea + Atlantis, run all scenarios
make -C e2e test                       # re-run scenarios against a running environment
make -C e2e test SCENARIOS=01-plan-apply
make -C e2e clean                      # tear down, including volumes
```

Requirements: Docker with Compose v2, Go, `git`, `curl` and `jq`.

## Layout

| Path                   | Purpose                                                                 |
| ---------------------- | ----------------------------------------------------------------------- |
| `Dockerfile.atlantis`  | Pinned Atlantis image with pinned `terraform`, `terramate` and our binary |
| `docker-compose.yaml`  | Gitea and Atlantis services                                             |
| `repos.yaml`           | Server-side Atlantis config: the pre-workflow hook and server workflows |
| `fixtures/`            | Terramate project pushed to `main` of the test repository during setup  |
| `scenarios/*.sh`       | One scenario per file, run in file name order                           |
| `scripts/setup.sh`     | Creates the Gitea users, tokens, test repository and webhook            |
| `scripts/lib.sh`       | Helpers available to scenarios                                          |
| `scripts/run-test.sh`  | Runs scenarios, prints a summary and dumps logs on failure              |

## Adding a scenario

Create `scenarios/NN-name.sh` defining `scenario_run`:

```bash
# shellcheck shell=bash
scenario_run() {
	new_branch my-scenario          # fresh clone + branch, cwd is the repo
	add_stack stacks/new new        # or touch_stack stacks/dev
	cat >.terramate-atlantis.yaml <<'EOF'
depends_on: true                    # tune the generator for this PR only
EOF
	open_pr "E2E: my scenario"      # Atlantis autoplans on open

	wait_for_comment 'Ran Plan|Error'
	assert_project new stacks/new
	assert_no_errors

	comment "atlantis apply"
	wait_for_comment 'Ran Apply|Error'
	assert_comment_contains "Apply complete!"
}
```

`advance_main` pushes a commit to `main` so the pull request branch is behind
its base (call it before changing files; it commits the working tree).
`push_commit` updates an open pull request; follow it with
`comment "atlantis plan"`. `assert_comment_count TEXT N` checks how often a
string appears, e.g. that a resource is planned exactly once.

`assert_generated_contains` checks the `atlantis.yaml` the hook generated for
the pull request (read from the Atlantis container), for settings that do not
show up in comments such as `depends_on`.

Each scenario runs in its own subshell; any failing command or assertion fails
it. Its pull request is unlocked and closed afterwards, so scenarios should
touch different stacks or rely on that cleanup. Pushing more commits to an
open pull request does not trigger autoplan (the sync event is not
subscribed); comment `atlantis plan` instead.

# Workflows

An Atlantis [workflow](https://www.runatlantis.io/docs/custom-workflows.html) defines the commands Atlantis runs to plan and apply a project. This tool can assign a workflow to each project, and it can generate a `terramate` workflow that runs Terraform through [`terramate run`](https://terramate.io/docs/cli/reference/cmdline/run).

```sh
terramate-atlantis-config generate --generate-workflows --output atlantis.yaml
```

With [`testdata/simple`](../testdata/simple) and `--no-header`, this produces:

```yaml
version: 3
automerge: false
parallel_plan: true
parallel_apply: true
projects:
  - name: stack-a
    dir: stack-a
    workflow: terramate
    autoplan:
      enabled: true
      when_modified:
        - '*.tf'
        - '*.tf.json'
        - .terraform.lock.hcl
workflows:
  terramate:
    apply:
      steps:
        - run: terramate run --no-recursive --disable-safeguards=git -- terraform apply "$PLANFILE"
    plan:
      steps:
        - run: terramate run --no-recursive --disable-safeguards=git -- terraform init -input=false
        - run: terramate run --no-recursive --disable-safeguards=git -- terraform plan -input=false -out="$PLANFILE"
```

> [!IMPORTANT]
> Atlantis only accepts this file if the server-side config sets `allowed_overrides: [workflow]` and `allow_custom_workflows: true`. The Atlantis server also needs the `terramate` CLI and `terraform` on its `PATH`. See [Setup](setup.md#server-side-repo-config).

## How a project gets its workflow

Each project gets the first workflow that applies:

1. the stack's `atlantis_workflow` [global](stack-globals.md);
2. a `--tag-workflow` / `tag_workflows` mapping for one of the stack's tags (the first mapped tag, in the order the stack declares its tags);
3. `--workflow` / `workflow`;
4. the generated `terramate` workflow, when `--generate-workflows` is on.

A project that matches none of these has no `workflow` key, and Atlantis uses its default workflow.

Map tags to workflows with a comma-separated list, or repeat the flag:

```sh
terramate-atlantis-config generate --tag-workflow production=prod-wf,staging=staging-wf
```

`--tag-requirements` maps tags to apply requirements in the same order; see [Apply requirements](stack-globals.md#apply-requirements).

## Why the generated workflow uses these flags

- **`--no-recursive`**: every stack is its own Atlantis project, so a parent stack must not also run its nested child stacks.
- **`--disable-safeguards=git`**: Atlantis plans pull requests whose branch is usually behind the default branch. That makes Terramate's `git-out-of-sync` [safeguard](https://terramate.io/docs/cli/orchestration/safeguards) refuse to run (`current HEAD is out-of-date with the remote base branch`). Terramate skips the `git-untracked` and `git-uncommitted` checks under `--no-recursive`, so disabling all of `git` changes nothing else. `outdated-code` stays enabled.

Pick a different set of safeguards to disable with `--workflow-disable-safeguards`. It accepts `all`, `none`, `git`, `git-untracked`, `git-uncommitted`, `git-out-of-sync` and `outdated-code`:

```sh
# Disable only these two
terramate-atlantis-config generate --generate-workflows --workflow-disable-safeguards git-out-of-sync,git-untracked

# Keep every safeguard
terramate-atlantis-config generate --generate-workflows --workflow-disable-safeguards=
```

`--workflow-terramate-wrap=false` runs plain `terraform` commands instead of wrapping them in `terramate run`.

If you write your own workflows that call `terramate run`, add the same flags, or set this environment variable on the Atlantis server:

```sh
TM_DISABLE_SAFEGUARDS=git
```

## Merging workflows

The `workflows` block can come from three sources. They are merged by name, from lowest to highest precedence:

1. workflows kept from the existing output file with `--preserve-workflows`;
2. the generated `terramate` workflow (`--generate-workflows`), so a stale copy in the existing file is refreshed;
3. workflows defined under `workflows:` in the [config file](configuration.md#config-file), so you can replace the generated workflow by defining one with the same name.

When a config file workflow replaces a generated one, or a different copy kept from the output file, a warning names it on stderr. Replacing an identical copy that the previous run wrote, or refreshing the generated workflow, is silent.

`--preserve-workflows` has no effect when writing to stdout.

## Generated workflow details

- **Workspaces.** Atlantis passes a project's workspace to run steps as `$WORKSPACE`, not `TF_WORKSPACE`. So with `--create-workspace`, the generated workflow still runs in Terraform's `default` workspace.
- **Terraform version.** The generated workflow runs the `terraform` found on the Atlantis server's `PATH`. Atlantis installs the versions that projects ask for as `terraform<version>` and only uses them for its built-in steps. So a project's `terraform_version` (`--terraform-version`, `atlantis_terraform_version`) doesn't change the binary a generated workflow runs.

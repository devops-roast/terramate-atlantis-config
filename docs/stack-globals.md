# Stack globals

Set `atlantis_*` [Terramate globals](https://terramate.io/docs/cli/code-generation/variables/globals) in a stack to override the defaults for that stack's project. Globals are inherited, so a global in a parent directory applies to every stack below it.

From [`testdata/with_globals`](../testdata/with_globals):

```hcl
# prod/stack.tm.hcl
stack {
  name = "prod"
  id   = "a1b2c3d4-0000-0000-0000-000000000010"
}

globals {
  atlantis_workflow          = "production"
  atlantis_terraform_version = "1.5.0"
  atlantis_extra_deps        = ["../modules/**/*.tf"]
}
```

```yaml
projects:
  - name: prod
    dir: prod
    workflow: production
    terraform_version: 1.5.0
    autoplan:
      enabled: true
      when_modified:
        - '*.tf'
        - '*.tf.json'
        - .terraform.lock.hcl
        - ../modules/**/*.tf
```

A project with a `workflow` or `apply_requirements` needs the server to allow it. See [Server-side repo config](setup.md#server-side-repo-config).

## Globals

| Global                        | Type         | Description                                           |
| ----------------------------- | ------------ | ----------------------------------------------------- |
| `atlantis_skip`               | bool         | Generate no project for this stack                    |
| `atlantis_workflow`           | string       | Override workflow for this stack                      |
| `atlantis_autoplan`           | bool         | Override autoplan enabled/disabled                    |
| `atlantis_terraform_version`  | string       | Override Terraform version                            |
| `atlantis_when_modified`      | list(string) | Override when_modified patterns                       |
| `atlantis_extra_deps`         | list(string) | Additional when_modified entries merged with defaults (ignored when `atlantis_when_modified` is set) |
| `atlantis_apply_requirements` | list(string) | Override apply requirements                           |

Each global overrides the matching flag or config key for this stack: `atlantis_workflow` overrides `--workflow`, `atlantis_terraform_version` overrides `--terraform-version`, and so on. For how the workflow is chosen when tags map to workflows too, see [Workflows](workflows.md#how-a-project-gets-its-workflow).

## Apply requirements

A project's `apply_requirements` come from the first of these that is set:

1. the stack's `atlantis_apply_requirements` global;
2. a `--tag-requirements` / `tag_requirements` mapping for one of the stack's tags (the first mapped tag, in the order the stack declares its tags);
3. `--apply-requirements` / `apply_requirements`;
4. none: the project gets no `apply_requirements` key, and Atlantis uses the [server-side requirements](https://www.runatlantis.io/docs/command-requirements.html).

Repeat `--tag-requirements` for each tag:

```sh
terramate-atlantis-config generate \
  --tag-requirements production=approved,mergeable \
  --tag-requirements staging=mergeable
```

## Edge cases

- **Empty lists are explicit.** `atlantis_when_modified = []` and `atlantis_apply_requirements = []` are overrides, not "unset". `apply_requirements: []` is written to the project, which tells Atlantis to require nothing before apply, overriding the server-side requirements as well. Leave the global unset to fall back to tag requirements, `--apply-requirements`, and then the server-side defaults.
- **An empty default only clears the default.** An empty `--apply-requirements` or `apply_requirements: []` in the config file leaves the `apply_requirements` key out of the project, so Atlantis uses the server-side requirements.
- **Null is unset.** A global set to `null` (for example from `cond ? [...] : null`) counts as unset.
- **Wrong types fail the run:**

  ```text
  Error: discovering stacks: extracting atlantis globals for stack "/prod": atlantis_autoplan must be bool
  ```

- **Unknown globals are ignored with a warning.** Any other global starting with `atlantis_`, usually a typo, prints a warning on stderr and changes nothing. The warning lists up to three stacks that see the global:

  ```text
  Warning: unknown global "atlantis_workflows" (stacks: prod) is ignored; known atlantis globals: atlantis_apply_requirements, atlantis_autoplan, atlantis_extra_deps, atlantis_skip, atlantis_terraform_version, atlantis_when_modified, atlantis_workflow
  ```

- **Only selected stacks are checked.** A stack that a [filter](filtering.md) leaves out can't fail the run through invalid globals. `atlantis_skip` doesn't excuse invalid globals on a stack the filters select.
- **Skipped stacks** don't take part in [project naming](project-naming.md), and [ordering](dependencies.md) is followed through them.
- `atlantis_terraform_version` doesn't change the `terraform` binary that a [generated workflow](workflows.md#generated-workflow-details) runs.

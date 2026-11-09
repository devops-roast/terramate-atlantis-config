# Project naming

Every project gets a name derived from its stack's path. The default strategy, `auto-strip`, drops the path prefix that all stacks share. Pick another strategy with `--project-name-strategy`, or turn names off with `--create-project-name=false`.

Take a repository with these three stacks:

```text
stacks/aws/prod/eu-west-1/db/billing
stacks/aws/prod/eu-west-1/vpc
stacks/aws/staging/eu-west-1/db/billing
```

Each strategy names them like this (`last-n` with `--project-name-depth 2`):

| Strategy     | How it names a stack                                                         | `prod/…/db/billing`                    | `prod/…/vpc`                    | `staging/…/db/billing`                    |
| ------------ | ---------------------------------------------------------------------------- | -------------------------------------- | ------------------------------- | ----------------------------------------- |
| `auto-strip` | Strips the longest path prefix (whole segments) all stacks share             | `prod-eu-west-1-db-billing`            | `prod-eu-west-1-vpc`            | `staging-eu-west-1-db-billing`            |
| `stack-name` | Uses the stack's `name` (or its dir name), prefixed with up to 3 parent dirs | `prod-eu-west-1-db-billing`            | `aws-prod-eu-west-1-vpc`        | `staging-eu-west-1-db-billing`            |
| `last-n`     | Uses the last N path segments                                                | `prod-eu-west-1-db-billing`            | `eu-west-1-vpc`                 | `staging-eu-west-1-db-billing`            |
| `full`       | Full path, no stripping                                                      | `stacks-aws-prod-eu-west-1-db-billing` | `stacks-aws-prod-eu-west-1-vpc` | `stacks-aws-staging-eu-west-1-db-billing` |

With `last-n`, both billing stacks would be `db-billing`. The tool adds parent segments until the names differ; see [Names are unique](#names-are-unique).

```sh
terramate-atlantis-config generate --project-name-strategy last-n --project-name-depth 2
```

## Names are stable

Names are always computed against every stack in the repository, except stacks with `atlantis_skip`. So `--changed`, `--filter`, `--filter-path` and `--exclude-*` never rename a project. A project keeps its name whether or not a given run includes it.

## Names are unique

If two stacks would get the same name, the tool adds parent segments to the name the strategy produced until they differ. As a last resort it falls back to the full path, then to a numeric suffix.

## Names are valid

`/` and characters Atlantis rejects in project names (anything that isn't URL safe, such as spaces) are replaced with `-`. Leading `-` are dropped.

## Strip a prefix

`--project-name-prefix` strips a path prefix you choose:

```sh
terramate-atlantis-config generate --project-name-prefix stacks/aws/prod
```

With the stacks above, this gives `eu-west-1-db-billing` and `eu-west-1-vpc`. The staging stack isn't under the prefix, so it keeps its full path: `stacks-aws-staging-eu-west-1-db-billing`.

- With `auto-strip`, the prefix replaces the detected common prefix.
- With `stack-name`, the prefix is stripped before the strategy picks its parent dirs.
- `last-n` and `full` ignore it.
- It only strips whole path segments: `stacks/aws/prod` strips `stacks/aws/prod/vpc` but not `stacks/aws/production/vpc`.

## Edge cases

- **A single stack** has nothing to share a prefix with, so `auto-strip` keeps its full path: a repository whose only stack is `stacks/aws/production/vpc` names it `stacks-aws-production-vpc`.
- **The root stack.** A stack at the repository root gets `dir: .` and the name `root`. With `stack-name`, it gets its `name` attribute, if set.
- **`depends_on` needs names.** [`--depends-on`](dependencies.md) fails with `--create-project-name=false`, because Atlantis matches `depends_on` entries against project names.
- **Workspaces.** `--create-workspace` sets each project's workspace from its stack path, changed where needed into a valid Atlantis workspace name (`infra/vpc` becomes `infra-vpc`). The root stack keeps the default workspace. Generated workflows don't pass the workspace to Terraform; see [Workflows](workflows.md#generated-workflow-details).
- **Sort order.** `--sort-by name` sorts projects by name instead of dir. `--sort-by execution_order_group` needs `--execution-order-groups`, otherwise it sorts by dir.

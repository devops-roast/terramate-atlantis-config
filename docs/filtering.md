# Filtering

By default, every stack in the repository gets a project. Filters narrow that set by [Terramate tag](#tag-filters), by [path](#path-filters), or to the stacks [changed](#changed-stacks) in your branch.

With [`testdata/dependencies`](../testdata/dependencies), which has six stacks under `stacks/`:

```sh
# Only stacks tagged "network": stacks/network/dns, stacks/network/vpc
terramate-atlantis-config generate --root testdata/dependencies --filter network

# Only stacks under stacks/network: the same two
terramate-atlantis-config generate --root testdata/dependencies --filter-path stacks/network

# Everything except stacks under stacks/app:
# stacks/data/rds, stacks/network/dns, stacks/network/vpc, stacks/shared
terramate-atlantis-config generate --root testdata/dependencies --exclude-path stacks/app
```

Filters never change project names. See [Project naming](project-naming.md#names-are-stable).

## Tag filters

`--filter` keeps the stacks that match a tag filter, and `--exclude-tags` drops them. Both take the same syntax as the [`--tags` option of `terramate run`](https://terramate.io/docs/cli/reference/cmdline/run#--tags):

```sh
terramate-atlantis-config generate --filter network
terramate-atlantis-config generate --exclude-tags network
```

You set tags in the [stack configuration](https://terramate.io/docs/cli/stacks/configuration) with `tags = ["network"]`.

## Path filters

`--filter-path` keeps the stacks at or under the directories a glob matches, and `--exclude-path` drops them. A pattern selects everything nested under what it matches:

| Pattern  | Selects                                                    |
| -------- | ---------------------------------------------------------- |
| `infra`  | `infra`, `infra/vpc`, `infra/vpc/subnet`                   |
| `*/vpc`  | `prod/vpc`, `prod/vpc/subnet`                              |
| `**/vpc` | `vpc`, `prod/vpc`, `a/b/vpc`, and stacks nested under them |
| `.`      | only a stack at the repository root                        |
| `/`      | every stack                                                |

Rules:

- `*` matches within one path segment. `**` matches any number of segments, including none, so `**/vpc` also selects a top-level `vpc`.
- `?`, `[...]` classes and `\` escapes follow Go's [`path.Match`](https://pkg.go.dev/path#Match). On Windows, `\` is a path separator instead.
- Patterns are relative to `--root`: `infra`, `/infra`, `./infra` and `infra/` are the same filter. A pattern that leads outside the root, such as `../infra`, is an error.

## Changed stacks

`--changed` keeps only the stacks that changed compared with a git ref, `main` by default. Use it to generate a config for the stacks a pull request touches:

```sh
terramate-atlantis-config generate --changed --changed-base-ref main --output atlantis.yaml
```

[Terramate change detection](https://terramate.io/docs/cli/change-detection/) decides which stacks changed. Uncommitted and untracked files count as changes too.

You can combine `--changed` with short project names. Names don't depend on which stacks are included, so a project keeps its name whether or not it changed in a given pull request:

```yaml
# .terramate-atlantis.yaml
project_name_strategy: last-n
project_name_depth: 2
```

```sh
terramate-atlantis-config generate --changed --changed-base-ref origin/main --output atlantis.yaml
```

Pass `--changed` only in the runs that need it. `changed: true` in the config file would also apply to the [pre-commit hook](setup.md#pre-commit-hook) and to `--check`.

### Edge cases

- **The base ref must exist in the checkout.** A shallow or single-branch clone (the default of `actions/checkout`, and of Atlantis' `branch` checkout strategy) doesn't have it. See [`--changed` in shallow clones](setup.md#--changed-in-shallow-clones).
- **The comparison is direct, not from the merge base.** `--changed` compares the files at the base ref with the files at `HEAD`. If `main` moved on after you branched, stacks changed only on `main` count as changed as well. Pass the merge base to see only your branch's changes:

  ```sh
  terramate-atlantis-config generate --changed --changed-base-ref "$(git merge-base origin/main HEAD)"
  ```

- **On the base branch itself**, with a clean working tree, nothing has changed, so the generated config has no projects.

## How filters combine

A stack is generated only if it passes every include filter (`--filter`, `--filter-path`, `--changed`) and no exclude filter (`--exclude-tags`, `--exclude-path`). A stack with `atlantis_skip = true` is never generated.

```sh
# Stacks under infra, except infra/rds
terramate-atlantis-config generate --root testdata/multi --filter-path infra --exclude-path infra/rds
```

## Details

- **Filtered-out stacks can't fail the run** through invalid `atlantis_*` globals or `after`/`before` entries. A syntax error in any Terramate file still fails it, and `atlantis_skip` doesn't excuse invalid `atlantis_*` globals on a selected stack.
- **Ordering follows filtered-out stacks.** With [`--depends-on`](dependencies.md), a filtered-out stack isn't listed in `depends_on`, but ordering is followed through it.
- **The root stack.** A stack at the repository root is selected by `.` and by patterns that also match an empty path, such as `*` and `**`. It has no directory name, so `.*` or `?` don't select it. `.` selects only the root stack.

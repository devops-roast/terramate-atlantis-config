<sub>[Documentation](../README.md#documentation) › Troubleshooting</sub>

# Troubleshooting

Find your symptom below, then follow the link for its cause and fix.

| Symptom                                                                     | Where it shows up       | Go to                                                                        |
| --------------------------------------------------------------------------- | ----------------------- | ---------------------------------------------------------------------------- |
| `repo config not allowed to …`                                              | Atlantis comment        | [Atlantis rejects atlantis.yaml](#atlantis-rejects-atlantisyaml)             |
| `current HEAD is out-of-date with the remote base branch`                   | Atlantis plan output    | [Plan fails: current HEAD is out-of-date](#plan-fails-current-head-is-out-of-date) |
| `unknown revision or path not in the working tree`                          | `generate --changed`    | [`--changed` fails with "unknown revision"](#--changed-fails-with-unknown-revision) |
| Stacks you didn't touch are in the output                                   | `generate --changed`    | [`--changed` lists stacks you didn't touch](#--changed-lists-stacks-you-didnt-touch) |
| `Warning: unknown global …`, or a global changes nothing                    | `generate`              | [A global has no effect](#a-global-has-no-effect)                            |
| `atlantis.yaml is out of date` or `does not exist`                          | `generate --check`      | [`--check` fails](#--check-fails)                                            |
| `depends_on requires project names`                                         | `generate --depends-on` | [`--depends-on` fails without project names](#--depends-on-fails-without-project-names) |
| Anything else                                                               |                         | [Other errors](#other-errors)                                                |

<p align="center"><sub><b>Table 1.</b> Symptom index.</sub></p>

## Atlantis rejects atlantis.yaml

**Symptom.** Atlantis comments on the pull request with one of these errors and plans nothing:

```text
repo config not allowed to set 'workflow' key: server-side config needs 'allowed_overrides: [workflow]'
repo config not allowed to set 'apply_requirements' key: server-side config needs 'allowed_overrides: [apply_requirements]'
repo config not allowed to define custom workflows: server-side config needs 'allow_custom_workflows: true'
```

**Cause.** The generated file sets a key that the server-side `repos.yaml` doesn't let repositories set. Atlantis rejects the whole file when one project breaks the rule.

**Fix.** Allow the key in the server-side `repos.yaml`:

```yaml
repos:
  - id: /.*/
    allowed_overrides: [workflow, apply_requirements]
    allow_custom_workflows: true
```

[Server-side repo config](setup.md#server-side-repo-config) lists which flag or global produces each key. If you'd rather not allow a key, stop producing it.

## Plan fails: current HEAD is out-of-date

**Symptom.** A workflow step that calls `terramate run` fails in Atlantis with:

```text
current HEAD is out-of-date with the remote base branch
```

**Cause.** Atlantis plans the pull request branch, which is usually behind the default branch. Terramate's `git-out-of-sync` [safeguard](https://terramate.io/docs/cli/orchestration/safeguards) refuses to run in that case.

**Fix.** The [generated workflow](workflows.md) passes `--disable-safeguards=git` by default. If you passed `--workflow-disable-safeguards=` (keep all safeguards), remove it. If you wrote your own workflow, add `--no-recursive --disable-safeguards=git` to each `terramate run`, or set this on the Atlantis server:

```sh
TM_DISABLE_SAFEGUARDS=git
```

## `--changed` fails with "unknown revision"

**Symptom.** The run fails with an error that starts with `Error: discovering stacks: listing changed stacks against "main"` and contains:

```text
fatal: ambiguous argument 'main': unknown revision or path not in the working tree.
```

**Cause.** `--changed` compares against `--changed-base-ref`, and that ref isn't in the checkout. Shallow and single-branch clones don't have it. That is the default of `actions/checkout`, and of Atlantis' `branch` checkout strategy.

**Fix.** Fetch the history and the base branch, then compare against the remote ref. See [`--changed` in shallow clones](setup.md#--changed-in-shallow-clones).

## `--changed` lists stacks you didn't touch

**Symptom.** `--changed` includes stacks that changed on `main`, not in your branch.

**Cause.** `--changed` compares the base ref with `HEAD` directly, not from the point where your branch left the base. When `main` has moved on, its new changes show up as differences too.

**Fix.** Compare against the merge base:

```sh
terramate-atlantis-config generate --changed --changed-base-ref "$(git merge-base origin/main HEAD)"
```

## A global has no effect

**Symptom.** You set a global such as `atlantis_workflows = "production"`, and the project has no workflow. stderr shows:

```text
Warning: unknown global "atlantis_workflows" (stacks: prod) is ignored; known atlantis globals: atlantis_apply_requirements, atlantis_autoplan, atlantis_extra_deps, atlantis_skip, atlantis_terraform_version, atlantis_when_modified, atlantis_workflow
```

**Cause.** The name is misspelled. The tool reads only the globals listed in the warning and ignores any other `atlantis_*` global.

**Fix.** Use a name from [Stack globals](stack-globals.md#globals). If there's no warning, check that the stack isn't [filtered out](filtering.md) and that the global isn't set to `null`.

## `--check` fails

**Symptom.**

```text
atlantis.yaml is out of date, run the same command without --check and --diff to update it
```

or `atlantis.yaml does not exist, …`.

**Cause.** The committed file differs from what the tool generates now, or there's no file at the `--output` path.

**Fix.** Add `--diff` to see the difference. Then run the same command without `--check` and `--diff`, and commit the result:

```sh
terramate-atlantis-config generate --output atlantis.yaml --check --diff
terramate-atlantis-config generate --output atlantis.yaml
```

> [!TIP]
> If the diff is unexpected, check that CI passes the same flags you use locally. For example, `--root`, filters and `--format` all change the output. A `.terramate-atlantis.yaml` in `--root` applies to both runs.

## `--depends-on` fails without project names

**Symptom.**

```text
Error: depends_on requires project names: Atlantis matches depends_on entries against project names, so --depends-on cannot be used with --create-project-name=false
```

**Cause.** Atlantis matches `depends_on` entries against project names, so projects need names.

**Fix.** Remove `--create-project-name=false` (or `create_project_name: false`). To get shorter names, pick a [naming strategy](project-naming.md) instead.

## Other errors

| Message                                                                                   | Fix                                                                                        |
| ----------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------ |
| `--check and --diff require an output file (set --output or 'output' in the config file)` | Pass `--output atlantis.yaml`.                                                             |
| `config ".terramate-atlantis.yaml": line 1: unknown key "outptu" (valid keys: …)`         | Fix the key name in the [config file](configuration.md#keys).                              |
| `extracting atlantis globals for stack "/prod": atlantis_autoplan must be bool`           | Give the global the type listed in [Stack globals](stack-globals.md#globals).              |
| `path filter "../infra": the pattern leads outside the root`                              | Write [path filters](filtering.md#path-filters) relative to `--root`.                      |
| `WRN path /stacks/app/does-not-exist not found in configuration`                          | A stack's `after` or `before` names a missing path. It's ignored; fix or remove the entry. |

<p align="center"><sub><b>Table 2.</b> Other messages and how to fix them.</sub></p>

Didn't find your problem? Check the [FAQ](faq.md), or [open a bug report](https://github.com/devops-roast/terramate-atlantis-config/issues/new?template=bug.yml).

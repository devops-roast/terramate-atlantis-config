<sub>[Documentation](../README.md#documentation) › Commands</sub>

# Commands

`terramate-atlantis-config` has four commands. `generate` writes `atlantis.yaml` and is described in [Configuration](configuration.md). This page covers the other three and the exit codes.

| Command              | What it does                                   |
| -------------------- | ---------------------------------------------- |
| `generate`           | Generate `atlantis.yaml` from Terramate stacks |
| `validate <file>`    | Check an existing `atlantis.yaml`              |
| `version`            | Print the version, commit and build date       |
| `completion <shell>` | Print a shell completion script                |

<p align="center"><sub><b>Table 1.</b> Commands.</sub></p>

**On this page:** [validate](#validate) · [version](#version) · [completion](#completion) · [Exit codes](#exit-codes)

## validate

`validate` checks an `atlantis.yaml` for problems that make Atlantis reject it:

```console
$ terramate-atlantis-config validate atlantis.yaml
atlantis.yaml is valid
```

On problems, it lists each one on stderr and exits `1`. It isn't a full schema check. It checks the following:

| Check                                                                     | Message when it fails                                                                                            |
| ------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------- |
| `version` is `3`                                                          | `version must be 3`                                                                                              |
| every project has a `dir`                                                 | `projects[0]: dir is required`                                                                                   |
| no project dir contains `..`, even in `a..b`, since Atlantis rejects it   | `projects[6]: dir "a..b" must not contain '..'`                                                                  |
| project names are URL safe                                                | `invalid project name: bad name (must contain only URL safe characters)`                                         |
| project names are unique                                                  | `duplicate project name: app`                                                                                    |
| no project has `name: ""`                                                 | `projects[4]: name must not be empty when it is set`                                                             |
| projects that share a dir and workspace all have a name                   | `duplicate project dir: shared (workspace default); projects sharing a dir and workspace must all have a name`   |
| no named project has a glob dir (`*`, `?` or `[`)                         | `projects[5]: dir "stacks/*" is a glob pattern ('*', '?' or '['), so the project can't have a name`              |
| no project depends on itself                                              | `project "self" depends on itself`                                                                               |
| every `depends_on` entry names an existing project                        | `project "self" depends_on unknown project name "missing"`                                                       |

<p align="center"><sub><b>Table 2.</b> What <code>validate</code> checks, with the messages it prints.</sub></p>

When the file defines workflows, a project whose workflow isn't among them only gets a warning, since Atlantis also looks it up in the server-side repo config:

```text
Warning: project "wf" uses workflow "custom", which this file does not define; it must be defined in the server-side repo config
```

`validate` reads JSON files too.

## version

```console
$ terramate-atlantis-config version
terramate-atlantis-config 1.2.2 (commit: 8fb7e7ad57287d53dcbe9310948b02bab85d52f3, built: 2026-11-09T10:43:29Z)
```

| How it was built                       | Version            | Commit and date                 |
| -------------------------------------- | ------------------ | ------------------------------- |
| Release binary                         | the release        | set at build time               |
| `make build`                           | `git describe` of the checkout | set at build time   |
| `go build` in a git checkout           | a pseudo-version   | the commit, and its commit time as the date |
| `go install module@version`            | the module version | `unknown`                       |

<p align="center"><sub><b>Table 3.</b> What <code>version</code> prints for each kind of build.</sub></p>

## completion

`completion` prints a completion script for `bash`, `zsh`, `fish` or `powershell`. Run `terramate-atlantis-config completion <shell> --help` for how to load it permanently.

```sh
# Load zsh completions in the current shell
source <(terramate-atlantis-config completion zsh)
```

## Exit codes

| Code | Meaning                                                                                                    |
| ---- | ---------------------------------------------------------------------------------------------------------- |
| `0`  | Success. With `--check` or `--diff`, the output file is up to date                                         |
| `1`  | Any error; with `--check` or `--diff`, the output file is missing or stale; `validate` found problems      |

<p align="center"><sub><b>Table 4.</b> Exit codes for every command.</sub></p>

Errors print once on stderr, prefixed with `Error:`; `validate` lists the problems it finds without a prefix. Warnings print on stderr, prefixed with `Warning:`, and don't change the exit code.

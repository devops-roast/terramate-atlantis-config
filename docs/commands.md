# Commands

`terramate-atlantis-config` has four commands. `generate` writes `atlantis.yaml` and is described in [Configuration](configuration.md). This page covers the other three and the exit codes.

| Command                 | What it does                                   |
| ----------------------- | ---------------------------------------------- |
| `generate`              | Generate `atlantis.yaml` from Terramate stacks |
| `validate <file>`       | Check an existing `atlantis.yaml`              |
| `version`               | Print the version, commit and build date       |
| `completion <shell>`    | Print a shell completion script                |

## validate

`validate` checks an `atlantis.yaml` for problems that make Atlantis reject it:

```sh
terramate-atlantis-config validate atlantis.yaml
```

```text
atlantis.yaml is valid
```

On problems, it lists each one on stderr and exits `1`:

```text
projects[1]: dir "../outside" must not contain '..'
duplicate project name: app
project "app" depends_on unknown project name "network"
```

It isn't a full schema check. It checks that:

- `version` is `3`;
- every project has a `dir`;
- project names are valid and unique, and no project has `name: ""`;
- every project that shares a dir and workspace with another one has a name;
- no named project has a dir that is a glob pattern (containing `*`, `?` or `[`);
- no project dir contains `..`. Atlantis rejects it anywhere in a dir, even in `a..b`;
- every `depends_on` entry names an existing project other than the project itself.

When the file defines workflows, a project whose workflow isn't among them only gets a warning, since Atlantis also looks it up in the server-side repo config.

`validate` reads JSON files too.

## version

```sh
terramate-atlantis-config version
```

```text
terramate-atlantis-config 1.2.2 (commit: 8fb7e7ad57287d53dcbe9310948b02bab85d52f3, built: 2026-11-09T10:43:29Z)
```

Release binaries and `make build` set all three values. Other builds from a git checkout show the commit time as the build date. Builds made with `go install module@version` show `unknown` for both the commit and the date.

## completion

`completion` prints a completion script for `bash`, `zsh`, `fish` or `powershell`. Run `terramate-atlantis-config completion <shell> --help` for how to load it.

```sh
# Load zsh completions in the current shell
source <(terramate-atlantis-config completion zsh)
```

## Exit codes

| Code | Meaning                                                                                       |
| ---- | --------------------------------------------------------------------------------------------- |
| `0`  | Success. With `--check` or `--diff`, the output file is up to date                            |
| `1`  | Any error; with `--check` or `--diff`, the output file is missing or stale; `validate` found problems |

Errors print once on stderr, prefixed with `Error:`; `validate` lists the problems it finds without a prefix. Warnings print on stderr, prefixed with `Warning:`, and don't change the exit code.

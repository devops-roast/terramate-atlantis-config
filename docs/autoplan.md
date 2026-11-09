<sub>[Documentation](../README.md#documentation) › Autoplan</sub>

# Autoplan

Atlantis autoplans a project when a pull request modifies a file that matches one of the project's `autoplan.when_modified` patterns. The default patterns (`*.tf`, `*.tf.json`, `.terraform.lock.hcl`) only look at the stack's own directory. Terramate also feeds a stack from other places:

- globals and `generate_*` blocks in the config files of every parent directory,
- files pulled in with [`import`](https://terramate.io/docs/cli/reference/blocks/import),
- local Terraform modules the stack calls,
- the files in the stack's [`watch`](https://terramate.io/docs/cli/stacks/configuration) list.

If you commit Terramate's generated code, a change to a parent's globals also changes the generated `.tf` files of every stack below it, and the default patterns catch that. If Atlantis generates the code instead, for example in a pre-workflow hook, the pull request only touches the parent's `.tm.hcl` file. No project matches it, and Atlantis plans nothing.

`--when-modified-from-config` fixes this. It adds those files to every project's `when_modified`, so Atlantis plans the stacks a pull request affects, and only those. The tool still generates every project; Atlantis does the selection.

```sh
terramate-atlantis-config generate --when-modified-from-config
```

```yaml
# .terramate-atlantis.yaml
when_modified_from_config: true
```

It's off by default because it changes the generated file.

**On this page:** [Example](#example) · [What gets added](#what-gets-added) · [With stack globals](#with-stack-globals) · [Generated code in Atlantis](#generated-code-in-atlantis) · [Compared with `--changed`](#compared-with---changed) · [Edge cases](#edge-cases)

## Example

In [`testdata/when_modified_from_config`](../testdata/when_modified_from_config), the `vpc` stack sits five directories deep. Its parent `acme-development` imports a shared file, and the stack calls a local module that calls another one:

<table>
<tr>
<th>Files</th>
<th>Generated project</th>
</tr>
<tr>
<td valign="top">

```hcl
# stacks/aws/acme-development/account.tm.hcl
import {
  source = "/imports/aws.tm.hcl"
}

# imports/aws.tm.hcl
import {
  source = "/imports/common/tags.tm.hcl"
}

# stacks/aws/acme-development/eu-west-1/vpc/
# stack.tm.hcl
stack {
  name  = "vpc"
  watch = ["/config/vpc.json"]
}

# main.tf
module "vpc" {
  source = "../../../../../modules/vpc"
}

# modules/vpc/main.tf
module "subnets" {
  source = "../subnets"
}
```

</td>
<td valign="top">

```yaml
  - name: acme-development-eu-west-1-vpc
    dir: stacks/aws/acme-development/eu-west-1/vpc
    autoplan:
      enabled: true
      when_modified:
        - '*.tf'
        - '*.tf.json'
        - .terraform.lock.hcl
        - '*.tm.hcl'
        - '*.tm'
        - ../*.tm.hcl
        - ../*.tm
        - ../../*.tm.hcl
        - ../../*.tm
        - ../../../*.tm.hcl
        - ../../../*.tm
        - ../../../../*.tm.hcl
        - ../../../../*.tm
        - ../../../../../*.tm.hcl
        - ../../../../../*.tm
        - ../../../../../imports/aws.tm.hcl
        - ../../../../../imports/common/tags.tm.hcl
        - ../../../../../config/vpc.json
        - ../../../../../modules/subnets/**
        - ../../../../../modules/vpc/**
```

</td>
</tr>
</table>

<p align="center"><sub><b>Figure 1.</b> A stack, the files it depends on, and its generated project.</sub></p>

A pull request that changes only `stacks/aws/acme-development/account.tm.hcl` or only `imports/common/tags.tm.hcl` now plans both stacks under `acme-development`, and none under `acme-production`.

## What gets added

After the default patterns and `atlantis_extra_deps`, each project gets these entries, in this order:

| Source                                                                                     | Entry                                    | Example                             |
| ------------------------------------------------------------------------------------------ | ---------------------------------------- | ----------------------------------- |
| Config files of the stack directory and every parent up to `--root`                        | `*.tm.hcl` and `*.tm` for each directory | `../../*.tm.hcl`                    |
| Files pulled in by `import` blocks in those directories, and the files they import in turn | the exact path of each file              | `../../../../../imports/aws.tm.hcl` |
| The stack's `watch` files                                                                  | the exact path of each file              | `../../../../../config/vpc.json`    |
| Local modules the stack's `.tf` and `.tofu` files call, and the local modules those call   | `<module dir>/**`                        | `../../../../../modules/vpc/**`     |

<p align="center"><sub><b>Table 1.</b> The entries <code>--when-modified-from-config</code> adds to each project.</sub></p>

- **Config files are globs, the rest are exact paths.** Terramate reads every `.tm` and `.tm.hcl` file in the stack's directory and its parents, so a glob per directory matches exactly what Terramate reads. It also matches a file the pull request adds or deletes, and the generated file doesn't change when you add a config file. Imports and watch files are single files, so they are listed one by one.
- **Only the imports that apply to the stack.** An import in a parent directory applies to every stack below it. An import elsewhere in the tree doesn't, and a file nobody imports adds nothing.
- **Any file in a module directory counts**, as in Terramate's change detection: templates and policy files a module reads are covered too. A module nested in another listed module directory is covered by it and isn't listed again. Remote module sources (registry, git, S3 and so on) are ignored.
- **Paths are relative to the project directory** and use forward slashes, like every `when_modified` entry. Characters that Atlantis would read as pattern syntax (`*`, `?`, `[`, `\` and a leading `!`) are escaped in exact paths.
- **Duplicates are dropped.** When an entry appears twice, for example because `atlantis_extra_deps` lists it too, only the first one is kept.
- With Terramate's `tmgen` experiment on, the stack directory also gets `*.tmgen`.

## With stack globals

| Global                   | Effect                                                                                   |
| ------------------------ | ---------------------------------------------------------------------------------------- |
| `atlantis_extra_deps`    | Merged: its entries come after the defaults and before the derived ones                  |
| `atlantis_when_modified` | Replaces the whole list, as without the flag: nothing is derived for that stack          |
| `atlantis_autoplan`      | Unchanged: with `false`, the entries are written but Atlantis doesn't autoplan the stack |
| `atlantis_skip`          | Unchanged: the stack gets no project                                                     |

<p align="center"><sub><b>Table 2.</b> How the flag combines with the <a href="stack-globals.md">stack globals</a>.</sub></p>

## Generated code in Atlantis

The tool finds module calls in the `.tf` and `.tofu` files that exist when it runs. If you don't commit generated code, run `terramate generate` in the same pre-workflow hook, before the tool, so the generated files are there (the `terramate` CLI must be in your Atlantis image):

```yaml
# Server-side repos.yaml
repos:
  - id: /.*/
    pre_workflow_hooks:
      - run: terramate generate
        description: Generate Terraform code from Terramate config
      - run: terramate-atlantis-config generate --output atlantis.yaml --root .
        description: Generate atlantis.yaml from Terramate stacks
```

The [end-to-end tests](../e2e/repos.yaml) run this setup.

If you [commit `atlantis.yaml`](setup.md#a-commit-the-file), regenerate it when imports, `watch` lists or module calls change. The pre-commit hook and `--check` in CI already do.

## Compared with `--changed`

[`--changed`](filtering.md#changed-stacks) drops the projects of unchanged stacks, using Terramate's change detection. That detection maps a changed file up to the stack that contains it, and follows the stack's modules and `watch` files. It doesn't map a changed parent config file or imported file down to the stacks below it (checked with Terramate 0.17.3 and 0.17.4). With `--changed`, such a pull request generates no project for those stacks.

`--when-modified-from-config` keeps every project and lets Atlantis match the pull request's files. It follows the same modules and `watch` files as Terramate, so a stack that `terramate list --changed` reports because of one of them is autoplanned too. Files in the stack's own directory are still matched by `--when-modified` only: Terramate counts any file there as a change, such as `files/policy.json`, while the default patterns don't. Add such files to `--when-modified` or `atlantis_extra_deps`.

Don't combine the two flags: `--changed` removes the projects before their `when_modified` can match.

## Edge cases

<details>
<summary><b>What the tool can't see</b></summary>

- **Module calls in generated code** that hasn't been generated yet. See [Generated code in Atlantis](#generated-code-in-atlantis).
- **Module calls in `.tf.json` files.** Like Terramate's change detection, only `.tf` and `.tofu` files are read.
- **A file deleted from a glob import.** An import such as `source = "/imports/*.tm.hcl"` lists the files that exist when the tool runs. A pull request that only deletes one of them doesn't match.
- **Config in subdirectories of a stack** that aren't stacks themselves isn't included: Terramate doesn't apply it to the stack.

</details>

<details>
<summary><b>Modules it skips, with a warning</b></summary>

The tool doesn't fail the run over a module it can't follow, since Terraform reports the problem when it plans. It prints a warning on stderr and skips the module:

- a module outside `--root`, which Atlantis can't match against the pull request's files anyway,
- a source that isn't a directory,
- a `.tf` or `.tofu` file that doesn't parse.

```text
Warning: when_modified from config: skipping module "../../outside" called from one/main.tf: it is outside the root directory
```

A problem inside a shared module is reported once, however many stacks call it.

</details>

<details>
<summary><b>The root stack and nested stacks</b></summary>

- A stack at the root (`dir: .`) gets `*.tm.hcl` and `*.tm`, plus its imports, `watch` files and modules.
- A stack nested in another stack also gets the parent stack's config files, since it inherits the parent's globals. The parent stack doesn't get its children's files.
- Imports and `watch` files can't point outside the root: Terramate rejects them when it loads the config.

</details>

<details>
<summary><b>Size of the generated file</b></summary>

Each project gets two entries per directory between the stack and the root, plus one per import, `watch` file and module directory. A stack five directories deep gets twelve entries for its config files.

</details>

<sub>[Documentation](../README.md#documentation) › FAQ</sub>

# FAQ

Short answers to common questions, with links to the full explanation. Select a question to expand it.

**On this page:** [General](#general) · [Setup](#setup) · [Generated projects](#generated-projects) · [Ordering and workflows](#ordering-and-workflows)

## General

<details>
<summary><b>What does the tool change in my repository?</b></summary>

Only the output file you ask for, usually `atlantis.yaml`. It reads stack definitions, globals and git history, but it doesn't run Terraform and doesn't change your stack files.

</details>

<details>
<summary><b>How is it different from terragrunt-atlantis-config?</b></summary>

It's the same idea for a different tool. [terragrunt-atlantis-config](https://github.com/transcend-io/terragrunt-atlantis-config) reads Terragrunt modules; this tool reads Terramate stacks, their globals, tags and `after`/`before` ordering.

</details>

<details>
<summary><b>Do I need the <code>terramate</code> CLI?</b></summary>

Not to generate the file: the tool reads stacks through the Terramate SDK. The Atlantis server needs the CLI only if you use [generated workflows](workflows.md), which run Terraform through `terramate run`.

</details>

<details>
<summary><b>Which platforms are supported?</b></summary>

Release binaries exist for Linux, macOS and Windows, on amd64 and arm64. See [Install](../README.md#install).

</details>

## Setup

<details>
<summary><b>Should I commit <code>atlantis.yaml</code> or generate it on the Atlantis server?</b></summary>

Commit it (setup A) if you want to review every change to the file in pull requests. Generate it on the server (setup B) if you don't want a generated file in git. [Setup](setup.md) compares the two and walks through both.

</details>

<details>
<summary><b>Why does Atlantis reject the file?</b></summary>

The file sets a key that the server-side `repos.yaml` doesn't allow, such as a project `workflow` or a `workflows` block. See [Atlantis rejects atlantis.yaml](troubleshooting.md#atlantis-rejects-atlantisyaml).

</details>

<details>
<summary><b>How do I pin the version?</b></summary>

Use a full release tag instead of `v1`. For the pre-commit hook, set `rev: v1.2.2`. For the GitHub Action, `@v1.2.2` pins the action itself, and its `version` input (default `latest`) pins the binary it installs. Set both. See [Setup](setup.md#github-action).

</details>

<details>
<summary><b>Why does <code>--check</code> pass locally but fail in CI?</b></summary>

The two runs probably use different flags. `--root`, filters, `--format` and every other flag change the output. Run the same command in both places, and keep shared settings in [`.terramate-atlantis.yaml`](configuration.md#config-file). See [`--check` fails](troubleshooting.md#--check-fails).

</details>

## Generated projects

<details>
<summary><b>How do I leave a stack out?</b></summary>

Set `atlantis_skip` in the stack, or use an [exclude filter](filtering.md) for many stacks at once:

```hcl
globals {
  atlantis_skip = true
}
```

</details>

<details>
<summary><b>How do I autoplan a stack when a shared module changes?</b></summary>

For a local module (`source = "../modules/vpc"`), pass `--when-modified-from-config`: it adds every local module a stack calls, and the modules those call, to the stack's autoplan patterns. It covers parent globals and imported files too. See [Autoplan](autoplan.md).

To add files by hand, use `atlantis_extra_deps`. Patterns are relative to the stack's directory:

```hcl
globals {
  atlantis_extra_deps = ["../modules/**/*.tf"]
}
```

See [Stack globals](stack-globals.md).

</details>

<details>
<summary><b>Why doesn't a change to a parent's globals plan anything?</b></summary>

Atlantis plans a project when a changed file matches its `when_modified` patterns, which by default only cover `.tf` files in the stack's own directory. If you commit generated code, the generated files change too and match. If Atlantis generates the code, only the parent's `.tm.hcl` file changes. Pass `--when-modified-from-config` to add parent config files, imported files, local modules and `watch` files to every project. `--changed` doesn't help here: Terramate doesn't count a parent's config change as a change of the stacks below it. See [Autoplan](autoplan.md).

</details>

<details>
<summary><b>Why did my project names change?</b></summary>

Filters never rename projects. Adding or removing stacks can: with the default `auto-strip` strategy, the shared prefix depends on every stack. Set `--project-name-prefix` to keep names fixed. See [Names are stable](project-naming.md#names-are-stable).

</details>

<details>
<summary><b>Can I generate projects only for the stacks a pull request changed?</b></summary>

Yes, with `--changed`. Mind two things: the base ref must be in the checkout, and the comparison isn't from the merge base. See [Changed stacks](filtering.md#changed-stacks).

</details>

<details>
<summary><b>Can I get JSON instead of YAML?</b></summary>

Yes, with `--format json`. `validate` reads JSON files too.

</details>

<details>
<summary><b>Can I remove the "DO NOT EDIT" header?</b></summary>

Yes, with `--no-header` or `no_header: true` in the config file.

</details>

## Ordering and workflows

<details>
<summary><b>How do I make Atlantis apply stacks in order?</b></summary>

Declare the order with `after` and `before` in your stacks, then pass `--depends-on` and `--execution-order-groups`. See [Dependencies](dependencies.md).

</details>

<details>
<summary><b>Does a parent stack also run its nested child stacks?</b></summary>

Not with the generated workflow: it runs `terramate run --no-recursive`, because each child stack is its own Atlantis project. See [Workflows](workflows.md#why-the-generated-workflow-uses-these-flags).

</details>

<details>
<summary><b>Can each stack use a different Terraform version?</b></summary>

Set `atlantis_terraform_version` in the stack. Atlantis uses that version for its built-in steps. A generated workflow runs the `terraform` on the server's `PATH` instead. See [Generated workflow details](workflows.md#generated-workflow-details).

</details>

<details>
<summary><b>Can I replace the generated workflow with my own?</b></summary>

Yes. Keep `--generate-workflows` on, and define a workflow named `terramate` under `workflows:` in the config file. It replaces the generated one, and a warning on stderr says so. See [Merging workflows](workflows.md#merging-workflows).

</details>

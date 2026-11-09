# Contributing

Bug reports, ideas and pull requests are welcome.

| You want to…              | Do this                                                                                                                    |
| ------------------------- | -------------------------------------------------------------------------------------------------------------------------- |
| Report something broken   | [Open a bug report](https://github.com/devops-roast/terramate-atlantis-config/issues/new?template=bug.yml)                 |
| Suggest a feature         | [Open a feature request](https://github.com/devops-roast/terramate-atlantis-config/issues/new?template=feature.yml)        |
| Change the code or docs   | Fork the repository, create a branch, make your change with tests, and open a pull request against `main`                 |

<p align="center"><sub><b>Table 1.</b> Ways to contribute.</sub></p>

## Development setup

1. Install [mise](https://mise.jdx.dev/installing-mise.html).
2. Install the pinned tools (Go, golangci-lint, pre-commit and others from [`mise.toml`](mise.toml)):

   ```sh
   mise trust
   mise install
   ```

3. Build and install the binary from source:

   ```sh
   make build     # ./terramate-atlantis-config
   make install   # into $(go env GOPATH)/bin
   ```

## Checks

| Command      | What it runs                                                                    |
| ------------ | ------------------------------------------------------------------------------- |
| `make test`  | the tests                                                                       |
| `make lint`  | golangci-lint                                                                   |
| `make build` | builds the binary                                                               |
| `make check` | vet, format check, lint and tests (CI also runs `-race` and the Terramate CLI tests) |
| `make e2e`   | end-to-end tests against a real Atlantis and Gitea (needs Docker)               |
| `make help`  | lists every target                                                              |

<p align="center"><sub><b>Table 2.</b> Make targets you'll use most.</sub></p>

> [!IMPORTANT]
> Run `make check` before you open a pull request.

The end-to-end suite runs a real Atlantis server that generates `atlantis.yaml` in a pre-workflow hook. See [`e2e/README.md`](e2e/README.md) for its requirements and for how to add a scenario.

## Documentation

User documentation lives in [`README.md`](README.md) and [`docs/`](docs). [`docs_test.go`](docs_test.go) keeps it in step with the code:

| Check                                                                                                  | Fails when                                    |
| ------------------------------------------------------------------------------------------------------ | --------------------------------------------- |
| The flag tables in [`docs/configuration.md`](docs/configuration.md#cli-flags) list every `generate` flag | a flag or its default is missing or wrong     |
| The [config key table](docs/configuration.md#keys) lists every config file key                         | a key is missing, extra or maps to no flag    |
| The action tables in [`docs/setup.md`](docs/setup.md#github-action) match [`action.yml`](action.yml)    | an input, its default or an output differs    |
| The README's documentation table lists every page in `docs/`                                           | you add a page without linking it             |
| Every relative link and `#anchor` in `README.md`, `CONTRIBUTING.md` and `docs/*.md` resolves           | a file or heading was renamed or removed      |

<p align="center"><sub><b>Table 3.</b> What <code>docs_test.go</code> checks.</sub></p>

When you add a flag, a config key or an action input, update the matching table.

Formatting conventions for the docs:

- Lead with the common case. Put edge cases in a `<details>` block at the end of the section.
- Every example must be copy-pasteable, and every output must come from a real run.
- Put a caption under each figure and table: `<p align="center"><sub><b>Table 1.</b> …</sub></p>`. GitHub strips `<figure>` and `<caption>`.
- Diagrams are [Mermaid](https://mermaid.js.org) code blocks, which GitHub renders. Don't set colors, so they work in light and dark mode.
- No emoji.

## Commits and releases

Commit messages follow [Conventional Commits](https://www.conventionalcommits.org) (`feat: …`, `fix: …`, `docs: …`). [release-please](https://github.com/googleapis/release-please) reads them to write the [changelog](CHANGELOG.md) and cut releases.

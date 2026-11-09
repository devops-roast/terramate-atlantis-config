# Contributing

Bug reports, ideas and pull requests are welcome.

- [Open a bug report](https://github.com/devops-roast/terramate-atlantis-config/issues/new?template=bug.yml) if something doesn't work as expected.
- [Open a feature request](https://github.com/devops-roast/terramate-atlantis-config/issues/new?template=feature.yml) for new functionality.
- For a pull request, fork the repository, create a branch, make your change with tests, and open the pull request against `main`.

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

```sh
make test     # run tests
make lint     # run linters
make build    # build binary
make check    # vet + fmt + lint + test (CI also runs -race and the Terramate CLI tests)
make e2e      # end-to-end tests against real Atlantis + Gitea (needs Docker)
make help     # list all targets
```

Run `make check` before you open a pull request.

The end-to-end suite runs a real Atlantis server that generates `atlantis.yaml` in a pre-workflow hook. See [`e2e/README.md`](e2e/README.md) for its requirements and for how to add a scenario.

## Documentation

User documentation lives in [`README.md`](README.md) and [`docs/`](docs). [`docs_test.go`](docs_test.go) keeps it in step with the code:

- the flag tables in [`docs/configuration.md`](docs/configuration.md#cli-flags) must list every `generate` flag with its default;
- the config key table must list every key the config file accepts;
- the GitHub Action input and output tables in [`docs/setup.md`](docs/setup.md#github-action) must match [`action.yml`](action.yml);
- every relative link and `#anchor` in `README.md`, `CONTRIBUTING.md` and `docs/*.md` must resolve.

When you add a flag, a config key or an action input, update the matching table.

## Commits and releases

Commit messages follow [Conventional Commits](https://www.conventionalcommits.org) (`feat: …`, `fix: …`, `docs: …`). [release-please](https://github.com/googleapis/release-please) reads them to write the [changelog](CHANGELOG.md) and cut releases.

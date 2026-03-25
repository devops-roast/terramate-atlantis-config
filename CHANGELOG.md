# Changelog

## [1.1.0](https://github.com/devops-roast/terramate-atlantis-config/compare/v1.0.0...v1.1.0) (2026-03-25)


### Features

* add pre-commit hook definition ([ede7eef](https://github.com/devops-roast/terramate-atlantis-config/commit/ede7eef034fc8a36c67137bc32452a425f00a818))


### Bug Fixes

* regenerate golden files with correct yaml quoting ([a26ade1](https://github.com/devops-roast/terramate-atlantis-config/commit/a26ade10610fa8f57aba4220d4c09dd076c09a1f))
* resolve goreleaser v2 deprecation warnings ([1dc730b](https://github.com/devops-roast/terramate-atlantis-config/commit/1dc730bf7facaa2a2200f6166008cc1106efc5a0))
* use 2-space yaml indent and deduplicate testdata stack IDs ([daf2121](https://github.com/devops-roast/terramate-atlantis-config/commit/daf2121fe6e745f33739eb4b7c722b1502eb5136))

## 1.0.0 (2026-03-24)


### Features

* add atlantis config generation engine ([20e689a](https://github.com/devops-roast/terramate-atlantis-config/commit/20e689af0914501d337d8c3f44af92299a3b80c2))
* add atlantis YAML types ([79ebe7b](https://github.com/devops-roast/terramate-atlantis-config/commit/79ebe7b8b651996443b8b8afb3206f36fa989c17))
* add CLI skeleton with cobra commands ([29d4278](https://github.com/devops-roast/terramate-atlantis-config/commit/29d427844bbce7b1b2c5ff67938598d1e09f8fb8))
* add config file support (.terramate-atlantis.yaml) ([3acfee1](https://github.com/devops-roast/terramate-atlantis-config/commit/3acfee12caf5bb7c136ab1f82210e278b9a5fb58))
* add diff engine for check and diff modes ([9a2c4c8](https://github.com/devops-roast/terramate-atlantis-config/commit/9a2c4c83449170af125880f76f90ff5bb2ce9473))
* add generate command with full CLI flags ([6484396](https://github.com/devops-roast/terramate-atlantis-config/commit/6484396101d17f1dcaec8bab26949bfb4eed8ea3))
* add globals extraction from terramate cty values ([706a2a1](https://github.com/devops-roast/terramate-atlantis-config/commit/706a2a12a179e1031a42edd4b75f21b40d9c70ee))
* add stack discovery via Terramate SDK ([e234db6](https://github.com/devops-roast/terramate-atlantis-config/commit/e234db6a5fb94396fdd90ede5b05375a562fa4b5))
* add validate subcommand ([790ce48](https://github.com/devops-roast/terramate-atlantis-config/commit/790ce48f0672a2d5f07e6702648099fc89ea3eac))
* add workflow generation with terramate run wrapping ([67ed75e](https://github.com/devops-roast/terramate-atlantis-config/commit/67ed75ebe49207dd271ab4915c58f3b0baf838f9))


### Bug Fixes

* **ci:** build golangci-lint from source for Go 1.25 compat ([731af62](https://github.com/devops-roast/terramate-atlantis-config/commit/731af621c213d04421606768fb912aded6eb8d37))
* **ci:** enable experimental mise backend, skip release-please PRs ([ada6e29](https://github.com/devops-roast/terramate-atlantis-config/commit/ada6e2921eb6c8e02c87d74f55ca3fffdbfe610e))
* **ci:** replace deprecated linters exportloopref and tenv ([3cf5ffa](https://github.com/devops-roast/terramate-atlantis-config/commit/3cf5ffab6a09606a969392e0c438bc7d1e9d7441))
* **ci:** upgrade golangci-lint to v2, gate release-please on ci ([8ed5795](https://github.com/devops-roast/terramate-atlantis-config/commit/8ed5795d167e56daa6b688c442eaed7dd826b4df))
* **ci:** use go-version-file instead of hardcoded go versions ([331a8e6](https://github.com/devops-roast/terramate-atlantis-config/commit/331a8e654f4e59f43baa3e998e6cb06006a307ed))
* resolve golangci-lint findings ([b295a7c](https://github.com/devops-roast/terramate-atlantis-config/commit/b295a7c87fb8321250133d410f6ba3f22c46f8d5))

package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/moby/patternmatcher"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/devops-roast/terramate-atlantis-config/internal/atlantis"
	"github.com/devops-roast/terramate-atlantis-config/internal/discovery"
)

var update = flag.Bool("update", false, "rewrite testdata/*/expected.yaml golden files")

func TestIntegration(t *testing.T) {
	dependencyOpts := defaultOpts()
	dependencyOpts.DependsOn = true
	dependencyOpts.ExecutionOrderGroups = true

	fromConfigOpts := defaultOpts()
	fromConfigOpts.WhenModifiedFromConfig = true

	rootFromConfigOpts := dependencyOpts
	rootFromConfigOpts.WhenModifiedFromConfig = true

	tests := []struct {
		name    string
		fixture string
		opts    atlantis.Options
		// expected is the golden file in fixture, expected.yaml by default.
		expected string
	}{
		{name: "simple", fixture: "testdata/simple", opts: defaultOpts()},
		{name: "multi", fixture: "testdata/multi", opts: defaultOpts()},
		{name: "with_globals", fixture: "testdata/with_globals", opts: defaultOpts()},
		{name: "skip_stack", fixture: "testdata/skip_stack", opts: defaultOpts()},
		{name: "dependencies", fixture: "testdata/dependencies", opts: dependencyOpts},
		{name: "root_stack", fixture: "testdata/root_stack", opts: dependencyOpts},
		{name: "when_modified_from_config", fixture: "testdata/when_modified_from_config", opts: fromConfigOpts},
		{
			name: "root_stack_when_modified_from_config", fixture: "testdata/root_stack", opts: rootFromConfigOpts,
			expected: "expected_when_modified_from_config.yaml",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := copyFixture(t, tt.fixture)
			initGitRepo(t, tmpDir)

			result, err := discovery.Discover(discovery.DiscoverOptions{
				RootDir:                tmpDir,
				WhenModifiedFromConfig: tt.opts.WhenModifiedFromConfig,
			})
			require.NoError(t, err)

			opts := tt.opts
			opts.AllStacks = result.AllStacks
			cfg, err := atlantis.Generate(result.Stacks, opts)
			require.NoError(t, err)
			got, err := atlantis.MarshalYAML(cfg, false)
			require.NoError(t, err)

			expectedFile := tt.expected
			if expectedFile == "" {
				expectedFile = "expected.yaml"
			}
			expectedPath, err := filepath.Abs(filepath.Join(tt.fixture, expectedFile))
			require.NoError(t, err)
			if *update {
				require.NoError(t, os.WriteFile(expectedPath, got, 0o644))
			}
			expected, err := os.ReadFile(expectedPath)
			require.NoError(t, err)

			require.Equal(t, string(expected), string(got))
		})
	}
}

// TestCLIConfigFileGolden drives the binary purely from a config file that
// uses the flag-equivalent keys (generate_workflows, tag_requirements lists,
// workflow_disable_safeguards, ...) and compares against a golden file.
func TestCLIConfigFileGolden(t *testing.T) {
	const fixture = "testdata/config_workflows"
	dir := copyFixture(t, fixture)
	initGitRepo(t, dir)

	res := runCLI(t, dir, "generate")
	require.Equal(t, 0, res.exitCode, res.stderr)
	require.Empty(t, res.stderr)

	expectedPath, err := filepath.Abs(filepath.Join(fixture, "expected.yaml"))
	require.NoError(t, err)
	if *update {
		require.NoError(t, os.WriteFile(expectedPath, []byte(res.stdout), 0o644))
	}
	expected, err := os.ReadFile(expectedPath)
	require.NoError(t, err)
	require.Equal(t, string(expected), res.stdout)

	require.NoError(t, os.WriteFile(filepath.Join(dir, "atlantis.yaml"), []byte(res.stdout), 0o644))
	validate := runCLI(t, dir, "validate", "atlantis.yaml")
	require.Equal(t, 0, validate.exitCode, validate.stderr)
}

// The tests below run the real binary against fixture repositories.

var (
	buildOnce sync.Once
	binDir    string
	binPath   string
	errBuild  error
)

func TestMain(m *testing.M) {
	flag.Parse()
	code := m.Run()
	if binDir != "" {
		_ = os.RemoveAll(binDir)
	}
	os.Exit(code)
}

// buildBinary compiles the CLI once per test run.
func buildBinary() {
	dir, err := os.MkdirTemp("", "tac-bin-")
	if err != nil {
		errBuild = err
		return
	}
	binDir = dir
	binPath = filepath.Join(dir, "terramate-atlantis-config")
	out, err := exec.Command("go", "build", "-o", binPath, ".").CombinedOutput()
	if err != nil {
		errBuild = errors.New(string(out))
	}
}

func cliBinary(t *testing.T) string {
	t.Helper()

	buildOnce.Do(buildBinary)
	require.NoError(t, errBuild, "building CLI binary")
	return binPath
}

type cliResult struct {
	stdout   string
	stderr   string
	exitCode int
}

func runCLI(t *testing.T, dir string, args ...string) cliResult {
	t.Helper()

	cmd := exec.Command(cliBinary(t), args...)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	code := 0
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		code = exitErr.ExitCode()
	} else {
		require.NoError(t, err)
	}

	return cliResult{stdout: stdout.String(), stderr: stderr.String(), exitCode: code}
}

func generateConfig(t *testing.T, dir string, args ...string) atlantis.Config {
	t.Helper()

	res := runCLI(t, dir, append([]string{"generate", "--no-header"}, args...)...)
	require.Equal(t, 0, res.exitCode, "generate failed: %s", res.stderr)

	var cfg atlantis.Config
	require.NoError(t, yaml.Unmarshal([]byte(res.stdout), &cfg))
	return cfg
}

// generateAndValidate writes the config to disk and checks that the
// validate command accepts it.
func generateAndValidate(t *testing.T, dir string, args ...string) atlantis.Config {
	t.Helper()

	cfg := generateConfig(t, dir, args...)
	res := runCLI(t, dir, append([]string{"generate", "--output", "atlantis.yaml"}, args...)...)
	require.Equal(t, 0, res.exitCode, "generate failed: %s", res.stderr)

	res = runCLI(t, dir, "validate", "atlantis.yaml")
	require.Equal(t, 0, res.exitCode, "validate rejected generated config: %s", res.stderr)
	return cfg
}

func projectsByDir(cfg atlantis.Config) map[string]atlantis.Project {
	m := make(map[string]atlantis.Project, len(cfg.Projects))
	for _, p := range cfg.Projects {
		m[p.Dir] = p
	}
	return m
}

func namesByDir(cfg atlantis.Config) map[string]string {
	m := make(map[string]string, len(cfg.Projects))
	for _, p := range cfg.Projects {
		m[p.Dir] = p.Name
	}
	return m
}

func TestCLIDependsOnUsesProjectNames(t *testing.T) {
	dir := copyFixture(t, "testdata/dependencies")
	initGitRepo(t, dir)

	cfg := generateAndValidate(t, dir, "--depends-on", "--execution-order-groups")
	projects := projectsByDir(cfg)

	names := map[string]struct{}{}
	for _, p := range cfg.Projects {
		names[p.Name] = struct{}{}
	}
	for _, p := range cfg.Projects {
		for _, dep := range p.DependsOn {
			_, ok := names[dep]
			require.True(t, ok, "project %s depends_on %q, which is not a project name", p.Name, dep)
		}
	}

	require.Equal(t, []string{"network-vpc"}, projects["stacks/network/dns"].DependsOn, "relative path after")
	require.Equal(t, []string{"network-dns", "network-vpc"}, projects["stacks/data/rds"].DependsOn, "tag: after")
	require.Equal(t, []string{"app-web", "data-rds", "network-dns"}, projects["stacks/app/api"].DependsOn,
		"absolute path after, tag: after and before on another stack")
	require.Equal(t, []string{"data-rds"}, projects["stacks/shared"].DependsOn, "directory after expands to the stacks below it")

	require.Equal(t, 0, projects["stacks/network/vpc"].ExecutionOrderGroup)
	require.Equal(t, 1, projects["stacks/network/dns"].ExecutionOrderGroup)
	require.Equal(t, 2, projects["stacks/data/rds"].ExecutionOrderGroup)
	require.Equal(t, 3, projects["stacks/app/api"].ExecutionOrderGroup)
}

func TestCLIDependsOnRequiresProjectNames(t *testing.T) {
	dir := copyFixture(t, "testdata/dependencies")
	initGitRepo(t, dir)

	res := runCLI(t, dir, "generate", "--depends-on", "--create-project-name=false")
	require.NotEqual(t, 0, res.exitCode)
	require.Contains(t, res.stderr, "depends_on requires project names")
}

func TestCLIDependencyCycleWarns(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, dir, "terramate.tm.hcl", "terramate {\n  config {\n  }\n}\n")
	writeTestFile(t, dir, "a/stack.tm.hcl", "stack {\n  name  = \"a\"\n  after = [\"../b\"]\n}\n")
	writeTestFile(t, dir, "b/stack.tm.hcl", "stack {\n  name  = \"b\"\n  after = [\"../a\"]\n}\n")
	initGitRepo(t, dir)

	res := runCLI(t, dir, "generate", "--execution-order-groups")
	require.Equal(t, 0, res.exitCode, res.stderr)
	require.Contains(t, res.stderr, "dependency cycle detected")
	require.Contains(t, res.stderr, "a, b")
}

func TestCLIRootStack(t *testing.T) {
	dir := copyFixture(t, "testdata/root_stack")
	initGitRepo(t, dir)

	cfg := generateAndValidate(t, dir, "--depends-on", "--create-workspace")
	projects := projectsByDir(cfg)

	root, ok := projects["."]
	require.True(t, ok, "root stack must use dir \".\", got %#v", cfg.Projects)
	require.Equal(t, "root", root.Name)
	require.Empty(t, root.Workspace, "root stack keeps the default workspace")
	require.Equal(t, []string{"root"}, projects["infra/vpc"].DependsOn)
}

// With stack-name, a root stack without a name attribute is named "root".
// Terramate fills an unset name with the stack directory's base name, which
// for the root stack is the checkout directory: the name would differ between
// a local clone and Atlantis' working copy (".../<pr>/default").
func TestCLIRootStackWithoutNameIsNamedRoot(t *testing.T) {
	for _, checkout := range []string{"my-repo", "default"} {
		dir := filepath.Join(t.TempDir(), checkout)
		require.NoError(t, os.MkdirAll(filepath.Join(dir, "infra", "vpc"), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "terramate.tm.hcl"),
			[]byte("terramate {\n  config {}\n}\n\nstack {}\n"), 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "infra", "vpc", "stack.tm.hcl"),
			[]byte("stack {}\n"), 0o644))
		initGitRepo(t, dir)

		cfg := generateAndValidate(t, dir, "--project-name-strategy", "stack-name")
		projects := projectsByDir(cfg)
		require.Equal(t, "root", projects["."].Name, "checkout dir %q", checkout)
		require.Equal(t, "infra-vpc", projects["infra/vpc"].Name, "checkout dir %q", checkout)
	}
}

func TestCLINamesAreStableAcrossFilters(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, dir, "terramate.tm.hcl", "terramate {\n  config {\n  }\n}\n")
	for _, stack := range []struct{ path, tag string }{
		{"stacks/aws/acme-production/eu-west-1/acm", "prod"},
		{"stacks/aws/acme-production/eu-west-1/dns", "prod"},
		{"stacks/aws/acme-development/eu-west-1/acm", "dev"},
		{"stacks/aws/acme-development/eu-west-1/dns", "dev"},
	} {
		writeTestFile(t, dir, stack.path+"/stack.tm.hcl",
			"stack {\n  name = \""+filepath.Base(stack.path)+"\"\n  tags = [\""+stack.tag+"\"]\n}\n")
		writeTestFile(t, dir, stack.path+"/main.tf", "locals {}\n")
	}
	initGitRepo(t, dir)

	for _, strategy := range []string{"auto-strip", "stack-name", "last-n", "full"} {
		t.Run(strategy, func(t *testing.T) {
			full := namesByDir(generateConfig(t, dir, "--project-name-strategy", strategy))
			require.Len(t, full, 4)

			for _, args := range [][]string{
				{"--filter-path", "stacks/aws/acme-development/eu-west-1/acm"},
				{"--filter", "dev"},
				{"--exclude-tags", "prod"},
				{"--exclude-path", "stacks/aws/acme-production/**"},
			} {
				filtered := namesByDir(generateConfig(t, dir, append(args, "--project-name-strategy", strategy)...))
				require.NotEmpty(t, filtered, "args %v", args)
				for dir, name := range filtered {
					require.Equal(t, full[dir], name, "name for %s changed with %v", dir, args)
				}
			}
		})
	}

	full := namesByDir(generateConfig(t, dir))
	require.Equal(t, "acme-development-eu-west-1-acm", full["stacks/aws/acme-development/eu-west-1/acm"])

	// --changed: only one stack differs from main.
	runGit(t, dir, "checkout", "-q", "-b", "feature")
	writeTestFile(t, dir, "stacks/aws/acme-development/eu-west-1/acm/main.tf", "locals { changed = true }\n")
	runGit(t, dir, "-c", "user.name=test", "-c", "user.email=test@test.com", "commit", "-qam", "change")

	changed := namesByDir(generateConfig(t, dir, "--changed"))
	require.Equal(t, map[string]string{
		"stacks/aws/acme-development/eu-west-1/acm": "acme-development-eu-west-1-acm",
	}, changed)
}

func TestCLIProjectNamePrefixMatchesWholeSegments(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, dir, "terramate.tm.hcl", "terramate {\n  config {\n  }\n}\n")
	writeTestFile(t, dir, "stacks/aws/prod/app/stack.tm.hcl", "stack {\n  name = \"app\"\n}\n")
	writeTestFile(t, dir, "stacks/aws/production/app/stack.tm.hcl", "stack {\n  name = \"app\"\n}\n")
	initGitRepo(t, dir)

	names := namesByDir(generateConfig(t, dir, "--project-name-prefix", "stacks/aws/prod"))
	require.Equal(t, "app", names["stacks/aws/prod/app"])
	require.Equal(t, "stacks-aws-production-app", names["stacks/aws/production/app"],
		"a prefix must not cut a directory name in half")
}

func TestCLINamesPassValidate(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, dir, "terramate.tm.hcl", "terramate {\n  config {\n  }\n}\n")
	writeTestFile(t, dir, "stacks/dev/v1.2/stack.tm.hcl", "stack {\n  name = \"v1.2\"\n}\n")
	writeTestFile(t, dir, "stacks/dev/my stack/stack.tm.hcl", "stack {\n  name = \"my stack\"\n}\n")
	writeTestFile(t, dir, "stacks/a/b/stack.tm.hcl", "stack {\n  name = \"b\"\n}\n")
	writeTestFile(t, dir, "stacks/a-b/stack.tm.hcl", "stack {\n  name = \"a-b\"\n}\n")
	initGitRepo(t, dir)

	for _, strategy := range []string{"auto-strip", "stack-name", "last-n", "full"} {
		t.Run(strategy, func(t *testing.T) {
			cfg := generateAndValidate(t, dir, "--project-name-strategy", strategy, "--project-name-depth", "1")
			for _, p := range cfg.Projects {
				require.True(t, atlantis.ValidProjectName(p.Name), "invalid name %q", p.Name)
			}
		})
	}

	names := namesByDir(generateConfig(t, dir))
	require.Equal(t, "dev-v1.2", names["stacks/dev/v1.2"])
	require.Equal(t, "dev-my-stack", names["stacks/dev/my stack"])
}

func TestCLIValidateRejectsUnknownDependsOn(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, dir, "atlantis.yaml", `version: 3
projects:
  - name: app
    dir: stacks/app
    depends_on:
      - stacks/network
  - name: network
    dir: stacks/network
`)

	res := runCLI(t, dir, "validate", "atlantis.yaml")
	require.Equal(t, 1, res.exitCode)
	require.Contains(t, res.stderr, `project "app" depends_on unknown project name "stacks/network"`)
}

// --- helpers ---

func defaultOpts() atlantis.Options {
	return atlantis.Options{
		Autoplan:          true,
		Parallel:          true,
		Automerge:         false,
		CreateProjectName: true,
		WhenModified:      []string{"*.tf", "*.tf.json", ".terraform.lock.hcl"},
	}
}

func writeTestFile(t *testing.T, root, rel, content string) {
	t.Helper()

	path := filepath.Join(root, filepath.FromSlash(rel))
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
}

func copyFixture(t *testing.T, fixture string) string {
	t.Helper()

	srcRoot, err := filepath.Abs(fixture)
	require.NoError(t, err)

	dstRoot := t.TempDir()
	err = filepath.WalkDir(srcRoot, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		rel, err := filepath.Rel(srcRoot, path)
		if err != nil {
			return err
		}
		// Golden files (expected.yaml, expected_*.yaml) are not part of the
		// Terramate project.
		if rel == "." || (strings.HasPrefix(rel, "expected") && filepath.Ext(rel) == ".yaml") {
			return nil
		}

		dstPath := filepath.Join(dstRoot, rel)
		if d.IsDir() {
			return os.MkdirAll(dstPath, 0o755)
		}

		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		if err := os.MkdirAll(filepath.Dir(dstPath), 0o755); err != nil {
			return err
		}

		return os.WriteFile(dstPath, content, 0o644)
	})
	require.NoError(t, err)

	return dstRoot
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()

	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, strings.TrimSpace(string(out)))
}

func initGitRepo(t *testing.T, dir string) {
	t.Helper()

	runGit(t, dir, "init", "-q", "-b", "main")
	runGit(t, dir, "add", ".")
	runGit(t, dir, "-c", "user.name=test", "-c", "user.email=test@test.com", "commit", "-q", "-m", "init")
}

// With --changed, a changed stack still runs after a changed stack it
// depends on through an unchanged one, matching
// `terramate list --changed --run-order`.
func TestCLIChangedKeepsTransitiveOrdering(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, dir, "terramate.tm.hcl", "terramate {\n  config {}\n}\n")
	writeTestFile(t, dir, "stacks/z/stack.tm.hcl", "stack {}\n")
	writeTestFile(t, dir, "stacks/m/stack.tm.hcl", "stack {\n  after = [\"../z\"]\n}\n")
	writeTestFile(t, dir, "stacks/a/stack.tm.hcl", "stack {\n  after = [\"../m\"]\n}\n")
	for _, s := range []string{"a", "m", "z"} {
		writeTestFile(t, dir, "stacks/"+s+"/main.tf", "\n")
	}
	initGitRepo(t, dir)
	runGit(t, dir, "checkout", "-q", "-b", "feature")
	writeTestFile(t, dir, "stacks/a/main.tf", "# changed\n")
	writeTestFile(t, dir, "stacks/z/main.tf", "# changed\n")
	runGit(t, dir, "-c", "user.name=test", "-c", "user.email=test@test.com", "commit", "-q", "-am", "change a and z")

	cfg := generateConfig(t, dir, "--changed", "--depends-on", "--execution-order-groups")

	byDir := projectsByDir(cfg)
	require.Len(t, byDir, 2)
	require.Equal(t, []string{"z"}, byDir["stacks/a"].DependsOn)
	require.Equal(t, 1, byDir["stacks/a"].ExecutionOrderGroup)
}

// Order entries are resolved once per stack, so Terramate's warning about
// a path that doesn't exist is logged once, not once per use.
func TestCLIOrderWarningsLoggedOnce(t *testing.T) {
	dir := copyFixture(t, "testdata/dependencies")
	initGitRepo(t, dir)

	res := runCLI(t, dir, "generate", "--depends-on")

	require.Equal(t, 0, res.exitCode, res.stderr)
	require.Equal(t, 1, strings.Count(res.stderr, "does-not-exist not found"), res.stderr)
}

// e2e/scripts/lib.sh runs under `set -euo pipefail`: assert_comment_count
// with an expected count of 0 used to abort silently, because grep exits 1
// when nothing matches.
func TestE2ELibAssertCommentCount(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not found")
	}
	assertCount := func(text, n string) error {
		script := `set -euo pipefail; source e2e/scripts/lib.sh; LAST_COMMENT="hello world, hello"; assert_comment_count "$1" "$2"`
		return exec.Command("bash", "-c", script, "bash", text, n).Run()
	}
	require.NoError(t, assertCount("nothere", "0"))
	require.NoError(t, assertCount("hello", "2"))
	require.Error(t, assertCount("hello", "0"))
	require.Error(t, assertCount("nothere", "1"))
}

// atlantisAutoplans returns the dirs of the projects Atlantis autoplans for a
// pull request that modifies files (repo-relative, with forward slashes),
// the way Atlantis matches when_modified (DetermineProjectsViaConfig in
// server/events/project_finder.go): each pattern is joined to the project dir,
// a leading '!' excludes, and a file matches when it or one of its parent
// directories matches.
func atlantisAutoplans(t *testing.T, cfg atlantis.Config, files ...string) []string {
	t.Helper()

	var planned []string
	for _, project := range cfg.Projects {
		if !project.Autoplan.Enabled {
			continue
		}
		patterns := make([]string, 0, len(project.Autoplan.WhenModified))
		for _, wm := range project.Autoplan.WhenModified {
			wm = strings.TrimSpace(wm)
			exclusion := strings.HasPrefix(wm, "!")
			wm = filepath.Join(project.Dir, strings.TrimPrefix(wm, "!"))
			if exclusion {
				wm = "!" + wm
			}
			patterns = append(patterns, wm)
		}
		pm, err := patternmatcher.New(patterns)
		require.NoError(t, err)
		for _, file := range files {
			match, err := pm.MatchesOrParentMatches(file)
			require.NoError(t, err)
			if match {
				planned = append(planned, project.Dir)
				break
			}
		}
	}
	return planned
}

// A pull request that changes only a parent directory's config, an imported
// file, a module, or a watched file contains no change in the stack's own
// directory. With --when-modified-from-config, Atlantis still autoplans the
// stacks that depend on it, and only those.
func TestCLIWhenModifiedFromConfigAutoplans(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Atlantis matches when_modified with Linux path semantics")
	}
	dir := copyFixture(t, "testdata/when_modified_from_config")
	initGitRepo(t, dir)

	const (
		dns     = "stacks/aws/acme-development/eu-west-1/dns"
		vpc     = "stacks/aws/acme-development/eu-west-1/vpc"
		app     = "stacks/aws/acme-production/eu-west-1/app"
		replica = "stacks/aws/acme-production/eu-west-1/app/replica"
		legacy  = "stacks/aws/acme-production/eu-west-1/legacy"
	)

	without := generateAndValidate(t, dir)
	require.Empty(t, atlantisAutoplans(t, without, "stacks/aws/acme-development/account.tm.hcl"),
		"without the flag, a parent globals change plans nothing")

	cfg := generateAndValidate(t, dir, "--when-modified-from-config")
	for _, tc := range []struct {
		name string
		file string
		want []string
	}{
		{"parent globals file", "stacks/aws/acme-development/account.tm.hcl", []string{dns, vpc}},
		{"new file in a parent directory", "stacks/aws/acme-development/eu-west-1/region.tm", []string{dns, vpc}},
		{"root config", "terramate.tm.hcl", []string{dns, vpc, app, replica}},
		{"imported file", "imports/aws.tm.hcl", []string{dns, vpc}},
		{"file imported by an imported file", "imports/common/tags.tm.hcl", []string{dns, vpc}},
		{"file nobody imports", "imports/unused.tm.hcl", nil},
		{"parent stack's config", app + "/stack.tm.hcl", []string{app, replica}},
		{"watched file", "config/vpc.json", []string{vpc}},
		{"module called by a module", "modules/subnets/main.tf", []string{vpc}},
		{"any file in a nested module", "modules/vpc/nested/templates/policy.json", []string{vpc}},
		{"atlantis_extra_deps entry", dns + "/records.yaml", []string{dns}},
		{"stack's own terraform", legacy + "/main.tf", []string{legacy}},
		{"unrelated file", "README.md", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, atlantisAutoplans(t, cfg, tc.file))
		})
	}

	// The config file key does the same, and the flag overrides it.
	writeTestFile(t, dir, ".terramate-atlantis.yaml", "when_modified_from_config: true\n")
	require.Equal(t, cfg, generateConfig(t, dir))
	require.Equal(t, without, generateConfig(t, dir, "--when-modified-from-config=false"))
}

// The stacks `terramate list --changed` reports for a change are autoplanned
// with --when-modified-from-config too. Terramate maps a changed file only up
// to its enclosing stack (and follows modules and watch files), never down to
// the stacks below a changed parent config file, which the flag also covers.
// Needs the terramate binary, see cmd/terramate_cli_test.go.
func TestCLIWhenModifiedFromConfigCoversTerramateChanged(t *testing.T) {
	if _, err := exec.LookPath("terramate"); err != nil {
		if os.Getenv("REQUIRE_TERRAMATE_CLI") != "" {
			t.Fatal("terramate not found in PATH and REQUIRE_TERRAMATE_CLI is set")
		}
		t.Skip("terramate not found in PATH")
	}
	if runtime.GOOS == "windows" {
		t.Skip("Atlantis matches when_modified with Linux path semantics")
	}

	dir := copyFixture(t, "testdata/when_modified_from_config")
	initGitRepo(t, dir)
	cfg := generateConfig(t, dir, "--when-modified-from-config")

	for i, tc := range []struct {
		file, content string
	}{
		{"stacks/aws/acme-development/account.tm.hcl", "\n# changed\n"},
		{"stacks/aws/acme-development/eu-west-1/region.tm", "globals {\n  region_label = \"euw1\"\n}\n"},
		{"imports/common/tags.tm.hcl", "\n# changed\n"},
		{"stacks/aws/acme-production/eu-west-1/app/stack.tm.hcl", "\n# changed\n"},
		{"stacks/aws/acme-production/eu-west-1/app/main.tf", "\n# changed\n"},
		{"config/vpc.json", "\n"},
		{"modules/subnets/main.tf", "\n# changed\n"},
		{"modules/vpc/nested/main.tf", "\n# changed\n"},
	} {
		t.Run(tc.file, func(t *testing.T) {
			runGit(t, dir, "checkout", "-q", "main")
			runGit(t, dir, "checkout", "-q", "-b", fmt.Sprintf("change-%d", i))
			path := filepath.Join(dir, filepath.FromSlash(tc.file))
			f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
			require.NoError(t, err)
			_, err = f.WriteString(tc.content)
			require.NoError(t, err)
			require.NoError(t, f.Close())
			runGit(t, dir, "add", "-A")
			runGit(t, dir, "-c", "user.name=test", "-c", "user.email=test@test.com", "commit", "-q", "-m", "change")

			list := exec.Command("terramate", "list", "--changed", "--git-change-base", "main")
			list.Dir = dir
			list.Env = append(os.Environ(), "CHECKPOINT_DISABLE=1")
			out, err := list.Output()
			require.NoError(t, err)
			changed := strings.Fields(string(out))

			planned := atlantisAutoplans(t, cfg, tc.file)
			require.Subset(t, planned, changed, "terramate list --changed reports a stack Atlantis wouldn't autoplan")
			t.Logf("terramate list --changed: %v; autoplanned: %v", changed, planned)
		})
	}
}

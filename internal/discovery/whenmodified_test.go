package discovery

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRelativePath(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct{ dir, target, want string }{
		{"a/b", "a/b/x.tm.hcl", "x.tm.hcl"},
		{"a/b", "a/x.tm.hcl", "../x.tm.hcl"},
		{"a/b", "c/d/x.tm.hcl", "../../c/d/x.tm.hcl"},
		{"a/b", "a/b", "."},
		{"a/b", ".", "../.."},
		{".", "imports/x.tm.hcl", "imports/x.tm.hcl"},
		{"", "imports/x.tm.hcl", "imports/x.tm.hcl"},
		{"a/bc", "a/b/x", "../b/x"},
		{"a/b", "a/b/c/d", "c/d"},
	} {
		assert.Equal(t, tc.want, relativePath(tc.dir, tc.target), "relativePath(%q, %q)", tc.dir, tc.target)
	}
}

func TestEscapePattern(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "../imports/aws.tm.hcl", escapePattern("../imports/aws.tm.hcl"))
	assert.Equal(t, `../a\*b/\?/\[x]/\\`, escapePattern(`../a*b/?/[x]/\`))
	assert.Equal(t, `\!draft/x.json`, escapePattern("!draft/x.json"))
	assert.Equal(t, "a/!b", escapePattern("a/!b"), "only a leading '!' means exclusion")
}

func TestOutermostDirs(t *testing.T) {
	t.Parallel()

	assert.Equal(t, []string{"a", "a-b", "b/c"}, outermostDirs([]string{"a", "a-b", "a/b", "a/b/c", "b/c", "b/c/d"}))
	assert.Equal(t, []string{"."}, outermostDirs([]string{".", "a"}))
	assert.Nil(t, outermostDirs(nil))
}

func configDepsOf(t *testing.T, root string, warnf func(string, ...any)) map[string][]string {
	t.Helper()
	result, err := Discover(DiscoverOptions{RootDir: root, WhenModifiedFromConfig: true, Warnf: warnf})
	require.NoError(t, err)
	deps := map[string][]string{}
	for _, stack := range result.Stacks {
		deps[stack.Dir] = stack.ConfigDeps
	}
	return deps
}

func TestConfigDepsAreOffByDefault(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeFile(t, filepath.Join(root, "terramate.tm.hcl"), "terramate {}\n")
	writeFile(t, filepath.Join(root, "a/stack.tm.hcl"), "stack {}\n")

	result, err := Discover(DiscoverOptions{RootDir: root})
	require.NoError(t, err)
	require.Len(t, result.Stacks, 1)
	assert.Nil(t, result.Stacks[0].ConfigDeps)
}

func TestConfigDepsRootAndNestedStacks(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeFile(t, filepath.Join(root, "terramate.tm.hcl"), "terramate {\n  config {}\n}\n\nstack {}\n")
	writeFile(t, filepath.Join(root, "a/stack.tm.hcl"), "stack {}\n")
	writeFile(t, filepath.Join(root, "a/b/stack.tm.hcl"), "stack {}\n")

	deps := configDepsOf(t, root, nil)
	assert.Equal(t, []string{"*.tm.hcl", "*.tm"}, deps["."])
	assert.Equal(t, []string{"*.tm.hcl", "*.tm", "../*.tm.hcl", "../*.tm"}, deps["a"])
	assert.Equal(t, []string{"*.tm.hcl", "*.tm", "../*.tm.hcl", "../*.tm", "../../*.tm.hcl", "../../*.tm"}, deps["a/b"])
}

func TestConfigDepsImports(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeFile(t, filepath.Join(root, "terramate.tm.hcl"), "terramate {}\n")
	// A glob source pulls in every matching file, listed one by one.
	writeFile(t, filepath.Join(root, "shared/b.tm.hcl"), "globals {\n  b = 1\n}\n")
	writeFile(t, filepath.Join(root, "shared/a.tm.hcl"), "globals {\n  a = 1\n}\n")
	// Relative sources are relative to the importing file's directory.
	writeFile(t, filepath.Join(root, "shared/nested/c.tm.hcl"), "import {\n  source = \"../../more/d.tm.hcl\"\n}\n")
	writeFile(t, filepath.Join(root, "more/d.tm.hcl"), "globals {\n  d = 1\n}\n")
	writeFile(t, filepath.Join(root, "envs/globals.tm.hcl"),
		"import {\n  source = \"/shared/*.tm.hcl\"\n}\n\nimport {\n  source = \"../shared/nested/c.tm.hcl\"\n}\n")
	writeFile(t, filepath.Join(root, "envs/dev/stack.tm.hcl"), "stack {}\n")
	// Imports in another branch of the tree don't apply.
	writeFile(t, filepath.Join(root, "other/globals.tm.hcl"), "import {\n  source = \"/more/d.tm.hcl\"\n}\n")
	writeFile(t, filepath.Join(root, "other/stack.tm.hcl"), "stack {}\n")

	deps := configDepsOf(t, root, nil)
	assert.Equal(t, []string{
		"*.tm.hcl", "*.tm", "../*.tm.hcl", "../*.tm", "../../*.tm.hcl", "../../*.tm",
		"../../more/d.tm.hcl",
		"../../shared/a.tm.hcl",
		"../../shared/b.tm.hcl",
		"../../shared/nested/c.tm.hcl",
	}, deps["envs/dev"])
	assert.Equal(t, []string{"*.tm.hcl", "*.tm", "../*.tm.hcl", "../*.tm", "../more/d.tm.hcl"}, deps["other"])
}

func TestConfigDepsWatchFilesAreEscaped(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeFile(t, filepath.Join(root, "terramate.tm.hcl"), "terramate {}\n")
	writeFile(t, filepath.Join(root, "config/[prod].json"), "{}\n")
	writeFile(t, filepath.Join(root, "config/b.json"), "{}\n")
	writeFile(t, filepath.Join(root, "app/stack.tm.hcl"),
		"stack {\n  watch = [\"/config/b.json\", \"../config/[prod].json\"]\n}\n")

	deps := configDepsOf(t, root, nil)
	assert.Equal(t, []string{"*.tm.hcl", "*.tm", "../*.tm.hcl", "../*.tm", `../config/\[prod].json`, "../config/b.json"}, deps["app"])
}

func TestConfigDepsTmgenExperiment(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeFile(t, filepath.Join(root, "terramate.tm.hcl"), "terramate {\n  config {\n    experiments = [\"tmgen\"]\n  }\n}\n")
	writeFile(t, filepath.Join(root, "app/stack.tm.hcl"), "stack {}\n")

	deps := configDepsOf(t, root, nil)
	assert.Equal(t, []string{"*.tm.hcl", "*.tm", "*.tmgen", "../*.tm.hcl", "../*.tm"}, deps["app"])
}

func TestConfigDepsModules(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeFile(t, filepath.Join(root, "terramate.tm.hcl"), "terramate {}\n")
	writeFile(t, filepath.Join(root, "modules/a/main.tf"),
		"module \"b\" {\n  source = \"../b\"\n}\n\nmodule \"c\" {\n  source = \"../c\"\n}\n")
	writeFile(t, filepath.Join(root, "modules/c/main.tf"), "locals {}\n")
	// A cycle between modules must not recurse forever.
	writeFile(t, filepath.Join(root, "modules/b/main.tofu"), "module \"a\" {\n  source = \"../a\"\n}\n")
	writeFile(t, filepath.Join(root, "app/local/main.tf"), "locals {}\n")
	writeFile(t, filepath.Join(root, "app/stack.tm.hcl"), "stack {}\n")
	writeFile(t, filepath.Join(root, "app/main.tf"), `module "a" {
  source = "../modules/a"
}

module "local" {
  source = "./local"
}

module "registry" {
  source = "hashicorp/consul/aws"
}

module "git" {
  source = "git::https://example.com/vpc.git"
}
`)
	writeFile(t, filepath.Join(root, "other/stack.tm.hcl"), "stack {}\n")
	writeFile(t, filepath.Join(root, "other/main.tf"), "module \"b\" {\n  source = \"../modules/b\"\n}\n")
	// .tf.json and other files are not read for module calls.
	writeFile(t, filepath.Join(root, "app/extra.tf.json"), `{"module": {"x": {"source": "../modules/x"}}}`)

	deps := configDepsOf(t, root, nil)
	assert.Equal(t, []string{
		"*.tm.hcl", "*.tm", "../*.tm.hcl", "../*.tm",
		// Sorted by their path from the root: app/local, modules/a, modules/b.
		"local/**", "../modules/a/**", "../modules/b/**", "../modules/c/**",
	}, deps["app"])
	// Entering the cycle from the other module finds the same modules.
	assert.Equal(t, []string{
		"*.tm.hcl", "*.tm", "../*.tm.hcl", "../*.tm",
		"../modules/a/**", "../modules/b/**", "../modules/c/**",
	}, deps["other"])
}

func TestConfigDepsSkipsUnreadableModulesWithWarnings(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeFile(t, filepath.Join(root, "terramate.tm.hcl"), "terramate {}\n")
	for _, stack := range []string{"one", "two"} {
		writeFile(t, filepath.Join(root, stack, "stack.tm.hcl"), "stack {}\n")
		writeFile(t, filepath.Join(root, stack, "main.tf"), `module "outside" {
  source = "../../outside"
}

module "missing" {
  source = "../modules/missing"
}

module "shared" {
  source = "../modules/shared"
}
`)
	}
	writeFile(t, filepath.Join(root, "modules/shared/main.tf"), "module \"broken\" {\n")

	var warnings []string
	warnf := func(format string, args ...any) { warnings = append(warnings, fmt.Sprintf(format, args...)) }
	deps := configDepsOf(t, root, warnf)

	want := []string{"*.tm.hcl", "*.tm", "../*.tm.hcl", "../*.tm", "../modules/shared/**"}
	assert.Equal(t, want, deps["one"])
	assert.Equal(t, want, deps["two"])

	// The broken module is reported once, although both stacks call it.
	require.Len(t, warnings, 5, strings.Join(warnings, "\n"))
	assert.Equal(t, `when_modified from config: skipping module "../../outside" called from one/main.tf: it is outside the root directory`,
		warnings[0])
	assert.Equal(t, `when_modified from config: skipping module "../modules/missing" called from one/main.tf: modules/missing is not a directory`,
		warnings[1])
	assert.True(t, strings.HasPrefix(warnings[2], "when_modified from config: skipping module calls in modules/shared/main.tf: modules/shared/main.tf:1,"),
		warnings[2])
	assert.Contains(t, warnings[3], `skipping module "../../outside" called from two/main.tf`)
	assert.Contains(t, warnings[4], `skipping module "../modules/missing" called from two/main.tf`)
}

func TestConfigDepsNotComputedWhenUnused(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeFile(t, filepath.Join(root, "terramate.tm.hcl"), "terramate {}\n")
	writeFile(t, filepath.Join(root, "override/stack.tm.hcl"),
		"stack {}\n\nglobals {\n  atlantis_when_modified = [\"*.tf\"]\n}\n")
	writeFile(t, filepath.Join(root, "override/main.tf"), "module \"x\" {\n  source = \"../../outside\"\n}\n")
	writeFile(t, filepath.Join(root, "skipped/stack.tm.hcl"), "stack {}\n\nglobals {\n  atlantis_skip = true\n}\n")
	writeFile(t, filepath.Join(root, "skipped/main.tf"), "module \"x\" {\n  source = \"../../outside\"\n}\n")

	var warnings []string
	deps := configDepsOf(t, root, func(format string, args ...any) { warnings = append(warnings, fmt.Sprintf(format, args...)) })
	assert.Nil(t, deps["override"])
	assert.Nil(t, deps["skipped"])
	assert.Empty(t, warnings)
}

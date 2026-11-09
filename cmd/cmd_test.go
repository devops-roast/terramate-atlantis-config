package cmd

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// result captures a single CLI invocation.
type result struct {
	code   int
	stdout string
	stderr string
}

func run(t *testing.T, args ...string) result {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := Run(args, &stdout, &stderr)
	return result{code: code, stdout: stdout.String(), stderr: stderr.String()}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
}

func gitCmd(t *testing.T, dir string, args ...string) {
	t.Helper()
	full := append([]string{"-c", "user.name=test", "-c", "user.email=test@example.com", "-c", "commit.gpgsign=false"}, args...)
	c := exec.Command("git", full...)
	c.Dir = dir
	out, err := c.CombinedOutput()
	require.NoError(t, err, "git %v: %s", args, out)
}

// newFixture creates a committed Terramate repository with two stacks and
// returns its root directory.
func newFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "terramate.tm.hcl"), "terramate {\n  config {}\n}\n")
	for _, name := range []string{"dev", "prod"} {
		dir := filepath.Join(root, "stacks", name)
		writeFile(t, filepath.Join(dir, "stack.tm.hcl"),
			"stack {\n  name = \""+name+"\"\n  tags = [\""+name+"\"]\n}\n")
		writeFile(t, filepath.Join(dir, "main.tf"), "terraform {}\n")
	}
	gitCmd(t, root, "init", "-q", "-b", "main")
	gitCmd(t, root, "add", "-A")
	gitCmd(t, root, "commit", "-q", "-m", "init")
	return root
}

func generateTo(t *testing.T, root, output string, extra ...string) result {
	t.Helper()
	args := append([]string{"generate", "--root", root, "--output", output}, extra...)
	return run(t, args...)
}

func TestGenerateToStdout(t *testing.T) {
	root := newFixture(t)

	res := run(t, "generate", "--root", root, "--no-header")

	require.Equal(t, 0, res.code, res.stderr)
	assert.Empty(t, res.stderr)
	assert.Contains(t, res.stdout, "dir: stacks/dev")
	assert.Contains(t, res.stdout, "dir: stacks/prod")
}

func TestGenerateWritesFileAndReportsCount(t *testing.T) {
	root := newFixture(t)
	out := filepath.Join(t.TempDir(), "custom.yaml")

	res := generateTo(t, root, out)

	require.Equal(t, 0, res.code, res.stderr)
	assert.Empty(t, res.stdout)
	assert.Equal(t, "Generated "+out+" with 2 projects\n", res.stderr)
	assert.FileExists(t, out)
}

func TestRuntimeErrorPrintedOnceWithoutUsage(t *testing.T) {
	root := newFixture(t)
	writeFile(t, filepath.Join(root, "stacks", "dev", "globals.tm.hcl"), "globals {\n  atlantis_skip = \"true\"\n}\n")

	res := run(t, "generate", "--root", root)

	assert.Equal(t, 1, res.code)
	assert.Empty(t, res.stdout)
	assert.Equal(t, 1, strings.Count(res.stderr, "atlantis_skip must be bool"), res.stderr)
	assert.Equal(t, 1, strings.Count(res.stderr, "Error:"), res.stderr)
	assert.NotContains(t, res.stderr, "Usage:")
	assert.NotContains(t, res.stderr, "Flags:")
}

func TestFlagErrorPointsToHelp(t *testing.T) {
	res := run(t, "generate", "--no-such-flag")

	assert.Equal(t, 1, res.code)
	assert.Equal(t, 1, strings.Count(res.stderr, "Error:"), res.stderr)
	assert.Contains(t, res.stderr, "unknown flag: --no-such-flag")
	assert.Contains(t, res.stderr, "Run 'terramate-atlantis-config generate --help' for usage")
	assert.NotContains(t, res.stderr, "Flags:")
}

func TestUnexpectedPositionalArgument(t *testing.T) {
	res := run(t, "generate", "extra")

	assert.Equal(t, 1, res.code)
	assert.Contains(t, res.stderr, "Error:")
	assert.NotContains(t, res.stderr, "Flags:")
}

func TestCheck(t *testing.T) {
	root := newFixture(t)

	t.Run("up to date", func(t *testing.T) {
		out := filepath.Join(t.TempDir(), "atlantis-out.yaml")
		require.Equal(t, 0, generateTo(t, root, out).code)

		res := generateTo(t, root, out, "--check")

		assert.Equal(t, 0, res.code, res.stderr)
		assert.Empty(t, res.stdout)
		assert.Equal(t, out+" is up to date\n", res.stderr)
	})

	t.Run("stale", func(t *testing.T) {
		out := filepath.Join(t.TempDir(), "atlantis-out.yaml")
		require.Equal(t, 0, generateTo(t, root, out).code)
		f, err := os.OpenFile(out, os.O_APPEND|os.O_WRONLY, 0o644)
		require.NoError(t, err)
		_, err = f.WriteString("# drift\n")
		require.NoError(t, err)
		require.NoError(t, f.Close())

		res := generateTo(t, root, out, "--check")

		assert.Equal(t, 1, res.code)
		assert.Empty(t, res.stdout)
		assert.Equal(t, out+" is out of date, run the same command without --check and --diff to update it\n", res.stderr)
		assert.NotContains(t, res.stderr, "Error:")
		assert.NotContains(t, res.stderr, "Usage:")
	})

	t.Run("missing file", func(t *testing.T) {
		out := filepath.Join(t.TempDir(), "missing.yaml")

		res := generateTo(t, root, out, "--check")

		assert.Equal(t, 1, res.code)
		assert.Equal(t, out+" does not exist, run the same command without --check and --diff to update it\n", res.stderr)
		assert.NoFileExists(t, out, "--check must not write the output file")
	})

	t.Run("requires output", func(t *testing.T) {
		res := run(t, "generate", "--root", root, "--check")

		assert.Equal(t, 1, res.code)
		assert.Contains(t, res.stderr, "--check and --diff require an output file")
	})
}

func TestDiff(t *testing.T) {
	root := newFixture(t)

	t.Run("no changes", func(t *testing.T) {
		out := filepath.Join(t.TempDir(), "atlantis.yaml")
		require.Equal(t, 0, generateTo(t, root, out).code)

		res := generateTo(t, root, out, "--diff")

		assert.Equal(t, 0, res.code, res.stderr)
		assert.Empty(t, res.stdout)
		assert.Empty(t, res.stderr)
	})

	t.Run("changes", func(t *testing.T) {
		out := filepath.Join(t.TempDir(), "atlantis.yaml")
		require.Equal(t, 0, generateTo(t, root, out).code)
		original, err := os.ReadFile(out)
		require.NoError(t, err)
		writeFile(t, out, strings.Replace(string(original), "dir: stacks/prod", "dir: stacks/old", 1))

		res := generateTo(t, root, out, "--diff")

		assert.Equal(t, 1, res.code)
		assert.Contains(t, res.stdout, "--- atlantis.yaml")
		assert.Contains(t, res.stdout, "-    dir: stacks/old")
		assert.Contains(t, res.stdout, "+    dir: stacks/prod")
		assert.Empty(t, res.stderr)
	})

	t.Run("missing file", func(t *testing.T) {
		out := filepath.Join(t.TempDir(), "atlantis.yaml")

		res := generateTo(t, root, out, "--diff")

		assert.Equal(t, 1, res.code)
		assert.Contains(t, res.stdout, "+    dir: stacks/dev")
	})
}

func TestCheckWithDiff(t *testing.T) {
	root := newFixture(t)

	t.Run("stale prints diff and fails", func(t *testing.T) {
		out := filepath.Join(t.TempDir(), "atlantis.yaml")
		writeFile(t, out, "version: 3\n")

		res := generateTo(t, root, out, "--check", "--diff")

		assert.Equal(t, 1, res.code)
		assert.Contains(t, res.stdout, "+    dir: stacks/dev")
		assert.Contains(t, res.stderr, out+" is out of date")
		assert.NotContains(t, res.stderr, "mutually exclusive")
	})

	t.Run("up to date passes", func(t *testing.T) {
		out := filepath.Join(t.TempDir(), "atlantis.yaml")
		require.Equal(t, 0, generateTo(t, root, out).code)

		res := generateTo(t, root, out, "--check", "--diff")

		assert.Equal(t, 0, res.code, res.stderr)
		assert.Empty(t, res.stdout)
		assert.Equal(t, out+" is up to date\n", res.stderr)
	})
}

func TestConfigFileValuesAreHonoredAndValidated(t *testing.T) {
	t.Run("output from config enables --check", func(t *testing.T) {
		root := newFixture(t)
		out := filepath.Join(t.TempDir(), "from-config.yaml")
		writeFile(t, filepath.Join(root, ".terramate-atlantis.yaml"), "output: "+out+"\n")

		require.Equal(t, 0, run(t, "generate", "--root", root).code)
		assert.FileExists(t, out)

		res := run(t, "generate", "--root", root, "--check")
		assert.Equal(t, 0, res.code, res.stderr)
		assert.Equal(t, out+" is up to date\n", res.stderr)
	})

	t.Run("format from config is used", func(t *testing.T) {
		root := newFixture(t)
		writeFile(t, filepath.Join(root, ".terramate-atlantis.yaml"), "format: json\n")

		res := run(t, "generate", "--root", root)

		require.Equal(t, 0, res.code, res.stderr)
		assert.True(t, strings.HasPrefix(res.stdout, "{"), res.stdout)
	})

	for _, tc := range []struct {
		name, config, want string
	}{
		{"invalid format", "format: xml\n", `invalid format "xml"`},
		{"invalid sort_by", "sort_by: size\n", `invalid sort-by "size"`},
		{"invalid strategy", "project_name_strategy: short\n", `invalid project-name-strategy "short"`},
		{"invalid depth", "project_name_depth: 0\n", `invalid project-name-depth 0`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := newFixture(t)
			writeFile(t, filepath.Join(root, ".terramate-atlantis.yaml"), tc.config)

			res := run(t, "generate", "--root", root)

			assert.Equal(t, 1, res.code)
			assert.Contains(t, res.stderr, tc.want)
			assert.Empty(t, res.stdout)
		})
	}

	t.Run("flag overrides config", func(t *testing.T) {
		root := newFixture(t)
		writeFile(t, filepath.Join(root, ".terramate-atlantis.yaml"), "format: xml\n")

		res := run(t, "generate", "--root", root, "--format", "yaml", "--no-header")

		assert.Equal(t, 0, res.code, res.stderr)
		assert.True(t, strings.HasPrefix(res.stdout, "version: 3"), res.stdout)
	})
}

func TestInvalidEnumFlags(t *testing.T) {
	root := newFixture(t)

	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"--format", "toml"}, `invalid format "toml", expected one of: yaml, json`},
		{[]string{"--sort-by", "bogus"}, `invalid sort-by "bogus", expected one of: dir, name, execution_order_group`},
		{[]string{"--project-name-strategy", "bogus"}, `invalid project-name-strategy "bogus", expected one of: auto-strip, stack-name, last-n, full`},
		{[]string{"--project-name-depth", "-1"}, `invalid project-name-depth -1, expected a value of at least 1`},
		{[]string{"--log-level", "loud"}, `invalid --log-level "loud", expected one of: trace, debug, info, warn, error, disabled`},
	} {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			res := run(t, append([]string{"generate", "--root", root}, tc.args...)...)

			assert.Equal(t, 1, res.code)
			assert.Equal(t, "Error: "+tc.want, strings.SplitN(res.stderr, "\n", 2)[0])
			assert.Empty(t, res.stdout)
		})
	}

	for _, value := range []string{"dir", "name", "execution_order_group"} {
		t.Run("valid sort-by "+value, func(t *testing.T) {
			res := run(t, "generate", "--root", root, "--sort-by", value)
			assert.Equal(t, 0, res.code, res.stderr)
		})
	}
}

func TestTagRequirementsFlag(t *testing.T) {
	root := newFixture(t)

	t.Run("comma separated requirements", func(t *testing.T) {
		res := run(t, "generate", "--root", root, "--no-header", "--tag-requirements", "prod=approved,mergeable")

		require.Equal(t, 0, res.code, res.stderr)
		assert.Contains(t, res.stdout, "    dir: stacks/prod\n    apply_requirements:\n      - approved\n      - mergeable\n")
		assert.NotContains(t, res.stdout, "dir: stacks/dev\n    apply_requirements")
	})

	t.Run("repeated flag", func(t *testing.T) {
		res := run(t, "generate", "--root", root, "--no-header",
			"--tag-requirements", "prod=approved,mergeable",
			"--tag-requirements", "dev=mergeable")

		require.Equal(t, 0, res.code, res.stderr)
		assert.Contains(t, res.stdout, "    dir: stacks/dev\n    apply_requirements:\n      - mergeable\n")
		assert.Contains(t, res.stdout, "    dir: stacks/prod\n    apply_requirements:\n      - approved\n      - mergeable\n")
	})

	t.Run("invalid entry", func(t *testing.T) {
		res := run(t, "generate", "--root", root, "--tag-requirements", "prod")

		assert.Equal(t, 1, res.code)
		assert.Contains(t, res.stderr, `invalid --tag-requirements entry "prod"`)
	})
}

func TestTagWorkflowCommaForm(t *testing.T) {
	root := newFixture(t)

	res := run(t, "generate", "--root", root, "--no-header", "--tag-workflow", "prod=prod-wf,dev=dev-wf")

	require.Equal(t, 0, res.code, res.stderr)
	assert.Contains(t, res.stdout, "    dir: stacks/dev\n    workflow: dev-wf\n")
	assert.Contains(t, res.stdout, "    dir: stacks/prod\n    workflow: prod-wf\n")
}

func TestParseTagRequirements(t *testing.T) {
	got, err := parseTagRequirements([]string{"prod= approved , mergeable,", " dev =mergeable"})
	require.NoError(t, err)
	assert.Equal(t, map[string][]string{
		"prod": {"approved", "mergeable"},
		"dev":  {"mergeable"},
	}, got)

	for _, entry := range []string{"prod", "=approved", "prod=", "prod=,,"} {
		_, err := parseTagRequirements([]string{entry})
		assert.Error(t, err, entry)
	}
}

func TestChangedDoesNotLeakTerramateLogs(t *testing.T) {
	root := newFixture(t)
	gitCmd(t, root, "checkout", "-q", "-b", "feature")
	writeFile(t, filepath.Join(root, "stacks", "prod", "main.tf"), "terraform {}\n# change\n")
	gitCmd(t, root, "commit", "-q", "-am", "change prod")

	res := run(t, "generate", "--root", root, "--changed", "--no-header")

	require.Equal(t, 0, res.code, res.stderr)
	assert.Empty(t, res.stderr, "Terramate SDK info and debug logs must not print by default")
	assert.Contains(t, res.stdout, "dir: stacks/prod")
	assert.NotContains(t, res.stdout, "dir: stacks/dev")

	debug := run(t, "generate", "--root", root, "--changed", "--no-header", "--log-level", "debug")
	require.Equal(t, 0, debug.code, debug.stderr)
	assert.NotEmpty(t, debug.stderr, "--log-level debug should surface Terramate SDK logs")
	assert.Equal(t, res.stdout, debug.stdout, "logs must never be written to stdout")
}

func TestValidateCommand(t *testing.T) {
	dir := t.TempDir()

	valid := filepath.Join(dir, "valid.yaml")
	writeFile(t, valid, "version: 3\nprojects:\n  - name: app\n    dir: app\n")
	res := run(t, "validate", valid)
	assert.Equal(t, 0, res.code, res.stderr)
	assert.Equal(t, valid+" is valid\n", res.stdout)

	invalid := filepath.Join(dir, "invalid.yaml")
	writeFile(t, invalid, "version: 2\nprojects:\n  - dir: app\n")
	res = run(t, "validate", invalid)
	assert.Equal(t, 1, res.code)
	assert.Equal(t, "version must be 3\n", res.stderr)

	res = run(t, "validate")
	assert.Equal(t, 1, res.code)
	assert.Contains(t, res.stderr, "accepts 1 arg(s), received 0")
}

// A file may use server-side workflows next to its own, as generate does
// with --generate-workflows and an atlantis_workflow global naming a server
// workflow; Atlantis accepts it, so validate must too.
func TestValidateAcceptsServerSideWorkflows(t *testing.T) {
	root := newFixture(t)
	writeFile(t, filepath.Join(root, "stacks", "prod", "globals.tm.hcl"), "globals {\n  atlantis_workflow = \"production\"\n}\n")
	out := filepath.Join(t.TempDir(), "atlantis.yaml")
	require.Equal(t, 0, generateTo(t, root, out, "--generate-workflows").code)

	res := run(t, "validate", out)

	assert.Equal(t, 0, res.code, res.stderr)
	assert.Equal(t, out+" is valid\n", res.stdout)
	assert.Equal(t, `Warning: project "prod" uses workflow "production", which this file does not define; it must be defined in the server-side repo config`+"\n", res.stderr)
}

func TestVersionCommand(t *testing.T) {
	res := run(t, "version")

	assert.Equal(t, 0, res.code)
	assert.Equal(t, "terramate-atlantis-config dev (commit: unknown, built: unknown)\n", res.stdout)
}

func TestCommandsHaveIndependentState(t *testing.T) {
	root := newFixture(t)

	first := run(t, "generate", "--root", root, "--no-header", "--format", "json")
	require.Equal(t, 0, first.code, first.stderr)
	second := run(t, "generate", "--root", root, "--no-header")
	require.Equal(t, 0, second.code, second.stderr)

	assert.True(t, strings.HasPrefix(second.stdout, "version: 3"), "flag values must not leak between runs")
}

// Comma-separated list flags accept spaces after the commas, like the config
// file and --tag-workflow/--tag-requirements do.
func TestListFlagsAreTrimmed(t *testing.T) {
	root := newFixture(t)

	res := run(t, "generate", "--root", root, "--no-header", "--generate-workflows",
		"--workflow-disable-safeguards", "git, outdated-code",
		"--when-modified", "*.tf, *.hcl,",
		"--apply-requirements", "approved, ,mergeable")

	require.Equal(t, 0, res.code, res.stderr)
	cfg := parsed(t, res.stdout)
	project := cfg.Projects[0]
	assert.Equal(t, []string{"*.tf", "*.hcl"}, project.Autoplan.WhenModified)
	require.NotNil(t, project.ApplyRequirements)
	assert.Equal(t, []string{"approved", "mergeable"}, *project.ApplyRequirements)
	assert.Contains(t, planRun(t, cfg, "terramate", 0), "--disable-safeguards=git,outdated-code -- ")

	// An explicitly empty list is still honored.
	res = run(t, "generate", "--root", root, "--no-header", "--when-modified=")
	require.Equal(t, 0, res.code, res.stderr)
	assert.Equal(t, []string{}, parsed(t, res.stdout).Projects[0].Autoplan.WhenModified)
}

// `when_modified: []` in the config file is an explicit empty list, the same
// as --when-modified=, not "unset".
func TestConfigEmptyWhenModifiedIsHonored(t *testing.T) {
	root := newFixture(t)
	writeFile(t, filepath.Join(root, ".terramate-atlantis.yaml"), "when_modified: []\nno_header: true\n")

	res := run(t, "generate", "--root", root)

	require.Equal(t, 0, res.code, res.stderr)
	assert.Equal(t, []string{}, parsed(t, res.stdout).Projects[0].Autoplan.WhenModified)

	// An unset key keeps the default patterns.
	writeFile(t, filepath.Join(root, ".terramate-atlantis.yaml"), "when_modified:\nno_header: true\n")
	res = run(t, "generate", "--root", root)
	require.Equal(t, 0, res.code, res.stderr)
	assert.Equal(t, []string{"*.tf", "*.tf.json", ".terraform.lock.hcl"}, parsed(t, res.stdout).Projects[0].Autoplan.WhenModified)
}

// A misspelled atlantis_* global must not be silently ignored. Globals are
// inherited, so a typo in a parent directory is reported once.
func TestUnknownAtlantisGlobalWarns(t *testing.T) {
	root := newFixture(t)
	writeFile(t, filepath.Join(root, "stacks", "globals.tm.hcl"), "globals {\n  atlantis_workflows = \"x\"\n}\n")

	res := run(t, "generate", "--root", root, "--no-header")

	require.Equal(t, 0, res.code, res.stderr)
	assert.Equal(t, 1, strings.Count(res.stderr, "Warning:"), res.stderr)
	assert.Contains(t, res.stderr, `unknown global "atlantis_workflows" (stacks: stacks/dev, stacks/prod) is ignored`)
	assert.Contains(t, res.stderr, "known atlantis globals: atlantis_apply_requirements,")
	assert.NotContains(t, res.stdout, "Warning", "warnings never mix with the generated config")
}

// `go install module@vX.Y.Z` builds have no -ldflags; the version must come
// from the embedded build information instead of printing "dev".
func TestVersionInfoFallsBackToBuildInfo(t *testing.T) {
	info := &debug.BuildInfo{
		Main: debug.Module{Version: "v1.2.0"},
		Settings: []debug.BuildSetting{
			{Key: "vcs.revision", Value: "abc123"},
			{Key: "vcs.time", Value: "2026-11-08T00:00:00Z"},
		},
	}
	version, commit, date := versionInfo(info)
	assert.Equal(t, []string{"v1.2.0", "abc123", "2026-11-08T00:00:00Z"}, []string{version, commit, date})

	// A local build without a module version keeps "dev".
	version, _, _ = versionInfo(&debug.BuildInfo{Main: debug.Module{Version: "(devel)"}})
	assert.Equal(t, "dev", version)
	version, commit, date = versionInfo(nil)
	assert.Equal(t, []string{"dev", "unknown", "unknown"}, []string{version, commit, date})

	// Values set with -ldflags win.
	defer func(v, c, d string) { Version, Commit, BuildDate = v, c, d }(Version, Commit, BuildDate)
	Version, Commit, BuildDate = "1.3.0", "def456", "today"
	version, commit, date = versionInfo(info)
	assert.Equal(t, []string{"1.3.0", "def456", "today"}, []string{version, commit, date})
}

// An empty tag flag value is an explicit "no mappings", like
// --apply-requirements= for lists: it clears the config file's mappings
// instead of failing, and a trailing comma adds no empty entry.
func TestEmptyTagFlagEntriesAreDropped(t *testing.T) {
	root := newFixture(t)
	writeFile(t, filepath.Join(root, ".terramate-atlantis.yaml"),
		"tag_workflows:\n  prod: prod-wf\ntag_requirements:\n  prod: [approved]\n")

	res := run(t, "generate", "--root", root, "--no-header", "--tag-requirements=", "--tag-workflow=")
	require.Equal(t, 0, res.code, res.stderr)
	assert.NotContains(t, res.stdout, "apply_requirements")
	assert.NotContains(t, res.stdout, "workflow:")

	res = run(t, "generate", "--root", root, "--no-header",
		"--tag-workflow", "prod=other-wf,", "--tag-requirements", " ", "--tag-requirements", "dev=mergeable")
	require.Equal(t, 0, res.code, res.stderr)
	assert.Contains(t, res.stdout, "    dir: stacks/prod\n    workflow: other-wf\n")
	assert.Contains(t, res.stdout, "    dir: stacks/dev\n    apply_requirements:\n      - mergeable\n")
}

// Invalid tag mappings from the config file are reported against the config
// key, not against a flag the user never passed.
func TestConfigTagMappingErrorsNameConfigKey(t *testing.T) {
	root := newFixture(t)
	for content, want := range map[string]string{
		"tag_requirements:\n  prod: []\n":  `tag_requirements: tag "prod" has no requirements`,
		"tag_requirements:\n  prod: ~\n":   `tag_requirements: tag "prod" has no requirements`,
		"tag_workflows:\n  prod: \"\"\n":   `tag_workflows: tag "prod" has no workflow`,
		"tag_workflows:\n  \" \": wf\n":    `tag_workflows: empty tag`,
		"tag_requirements:\n  \"\": [a]\n": `tag_requirements: empty tag`,
		// A mapping is passed on as tag=value, so "x=y" would turn into tag
		// "x" with workflow "y=wf".
		"tag_workflows:\n  x=y: wf\n":     `tag_workflows: tag "x=y" contains '='`,
		"tag_requirements:\n  x=y: [a]\n": `tag_requirements: tag "x=y" contains '='`,
	} {
		writeFile(t, filepath.Join(root, ".terramate-atlantis.yaml"), content)
		res := run(t, "generate", "--root", root)
		assert.Equal(t, 1, res.code, content)
		assert.Contains(t, res.stderr, "config file "+want, content)
		assert.NotContains(t, res.stderr, "--tag-", content)
	}
}

// The summary line agrees in number with the project count.
func TestGenerateSummaryForOneProject(t *testing.T) {
	root := newFixture(t)
	out := filepath.Join(t.TempDir(), "atlantis.yaml")

	res := generateTo(t, root, out, "--filter-path", "stacks/dev")

	require.Equal(t, 0, res.code, res.stderr)
	assert.Equal(t, "Generated "+out+" with 1 project\n", res.stderr)
}

// A value of the wrong type is not a syntax error.
func TestValidateReportsTypeErrorsAsParseErrors(t *testing.T) {
	path := filepath.Join(t.TempDir(), "atlantis.yaml")
	writeFile(t, path, "version: three\n")

	res := run(t, "validate", path)

	assert.Equal(t, 1, res.code)
	assert.Contains(t, res.stderr, "Error: parsing "+path+": ")
	assert.NotContains(t, res.stderr, "syntax")
}

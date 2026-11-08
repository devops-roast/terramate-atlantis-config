package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// These tests run the generated workflow commands with the real terramate
// (and terraform) binaries, the way Atlantis does: through `sh -c` from the
// project directory with PLANFILE set. They are skipped when the binaries are
// not installed, unless REQUIRE_TERRAMATE_CLI is set (as in CI), in which case
// a missing binary fails the test instead of silently skipping it.

func requireBinary(t *testing.T, name string) {
	t.Helper()
	if _, err := exec.LookPath(name); err != nil {
		if os.Getenv("REQUIRE_TERRAMATE_CLI") != "" {
			t.Fatalf("%s not found in PATH and REQUIRE_TERRAMATE_CLI is set", name)
		}
		t.Skipf("%s not found in PATH", name)
	}
}

// atlantisLikeClone mimics an Atlantis checkout of a pull request: a
// single-branch clone of the PR branch, whose default branch has since moved
// ahead (the common case for open PRs), plus an untracked atlantis.yaml
// written by the pre-workflow hook.
func atlantisLikeClone(t *testing.T) string {
	t.Helper()

	origin := t.TempDir()
	writeFile(t, filepath.Join(origin, "terramate.tm.hcl"), "terramate {\n  config {}\n}\n")
	writeFile(t, filepath.Join(origin, "parent", "stack.tm.hcl"), "stack {\n  name = \"parent\"\n}\n")
	writeFile(t, filepath.Join(origin, "parent", "main.tf"), "resource \"terraform_data\" \"this\" {\n  input = \"parent\"\n}\n")
	writeFile(t, filepath.Join(origin, "parent", "child", "stack.tm.hcl"), "stack {\n  name = \"child\"\n}\n")
	writeFile(t, filepath.Join(origin, "parent", "child", "main.tf"), "resource \"terraform_data\" \"this\" {\n  input = \"child\"\n}\n")
	writeFile(t, filepath.Join(origin, ".gitignore"), ".terraform/\n")
	gitCmd(t, origin, "init", "-q", "-b", "main")
	gitCmd(t, origin, "add", "-A")
	gitCmd(t, origin, "commit", "-q", "-m", "init")
	gitCmd(t, origin, "checkout", "-q", "-b", "feature")
	writeFile(t, filepath.Join(origin, "parent", "main.tf"), "resource \"terraform_data\" \"this\" {\n  input = \"parent-changed\"\n}\n")
	gitCmd(t, origin, "commit", "-q", "-am", "change parent")

	clone := filepath.Join(t.TempDir(), "repo")
	gitCmd(t, t.TempDir(), "clone", "-q", "--single-branch", "--branch", "feature", "file://"+origin, clone)

	// main moves on after the PR branch was created.
	gitCmd(t, origin, "checkout", "-q", "main")
	writeFile(t, filepath.Join(origin, "README.md"), "newer main\n")
	gitCmd(t, origin, "add", "-A")
	gitCmd(t, origin, "commit", "-q", "-m", "newer main")

	res := run(t, "generate", "--root", clone, "--output", filepath.Join(clone, "atlantis.yaml"), "--generate-workflows")
	require.Equal(t, 0, res.code, res.stderr)
	return clone
}

// workflowPrefix returns the `terramate run ... --` part of a generated step.
func workflowPrefix(t *testing.T, root string, args ...string) string {
	t.Helper()
	res := run(t, append([]string{"generate", "--root", root, "--no-header", "--generate-workflows"}, args...)...)
	require.Equal(t, 0, res.code, res.stderr)
	step := planRun(t, parsed(t, res.stdout), "terramate", 0)
	prefix, _, ok := strings.Cut(step, " terraform ")
	require.True(t, ok, step)
	return prefix
}

func shell(t *testing.T, dir, command string, env ...string) (string, error) {
	t.Helper()
	c := exec.Command("sh", "-c", command)
	c.Dir = dir
	c.Env = append(append(os.Environ(), "CHECKPOINT_DISABLE=1", "TF_IN_AUTOMATION=1"), env...)
	out, err := c.CombinedOutput()
	return string(out), err
}

func TestGeneratedWorkflowSafeguardsWithTerramateCLI(t *testing.T) {
	requireBinary(t, "terramate")
	clone := atlantisLikeClone(t)
	parent := filepath.Join(clone, "parent")

	t.Run("plain terramate run refuses a PR branch behind the default branch", func(t *testing.T) {
		out, err := shell(t, parent, "terramate run --no-recursive -- true")
		require.Error(t, err, out)
		assert.Contains(t, out, "out-of-date")
	})

	t.Run("keeping all safeguards reproduces the failure", func(t *testing.T) {
		out, err := shell(t, parent, workflowPrefix(t, clone, "--workflow-disable-safeguards=")+" true")
		require.Error(t, err, out)
	})

	t.Run("default generated workflow runs", func(t *testing.T) {
		out, err := shell(t, parent, workflowPrefix(t, clone)+" true")
		require.NoError(t, err, out)
	})

	t.Run("disabling only git-out-of-sync is enough", func(t *testing.T) {
		out, err := shell(t, parent, workflowPrefix(t, clone, "--workflow-disable-safeguards", "git-out-of-sync")+" true")
		require.NoError(t, err, out)
	})
}

func TestGeneratedWorkflowDoesNotRecurseWithTerramateCLI(t *testing.T) {
	requireBinary(t, "terramate")
	clone := atlantisLikeClone(t)
	parent := filepath.Join(clone, "parent")
	log := filepath.Join(t.TempDir(), "ran.log")

	out, err := shell(t, parent, workflowPrefix(t, clone)+` sh -c 'basename "$PWD" >> "$RAN_LOG"'`, "RAN_LOG="+log)
	require.NoError(t, err, out)

	ran, err := os.ReadFile(log)
	require.NoError(t, err)
	assert.Equal(t, "parent\n", string(ran), "only the project's own stack may run, not nested child stacks")

	// Contrast: without --no-recursive terramate also runs the child stack.
	recursive := filepath.Join(t.TempDir(), "recursive.log")
	out, err = shell(t, parent, `terramate run --disable-safeguards=git -- sh -c 'basename "$PWD" >> "$RAN_LOG"'`, "RAN_LOG="+recursive)
	require.NoError(t, err, out)
	ran, err = os.ReadFile(recursive)
	require.NoError(t, err)
	assert.Equal(t, "parent\nchild\n", string(ran))
}

func TestGeneratedWorkflowPlanApplyWithTerraform(t *testing.T) {
	requireBinary(t, "terramate")
	requireBinary(t, "terraform")
	clone := atlantisLikeClone(t)

	res := run(t, "generate", "--root", clone, "--no-header", "--generate-workflows")
	require.Equal(t, 0, res.code, res.stderr)
	cfg := parsed(t, res.stdout)

	for _, dir := range []string{"parent", "parent/child"} {
		t.Run(dir, func(t *testing.T) {
			projectDir := filepath.Join(clone, dir)
			planfile := filepath.Join(projectDir, "default.tfplan")
			env := []string{"PLANFILE=" + planfile}

			for i := 0; i < 2; i++ {
				step := planRun(t, cfg, "terramate", i)
				out, err := shell(t, projectDir, step, env...)
				require.NoError(t, err, "%s\n%s", step, out)
			}
			require.FileExists(t, planfile)

			wf := cfg.Workflows["terramate"].(map[string]interface{})
			apply := wf["apply"].(map[string]interface{})["steps"].([]interface{})[0].(map[string]interface{})["run"].(string)
			out, err := shell(t, projectDir, apply, env...)
			require.NoError(t, err, "%s\n%s", apply, out)
			assert.Contains(t, out, "Apply complete")
		})
	}
}

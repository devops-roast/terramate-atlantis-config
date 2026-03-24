package main

import (
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/devops-roast/terramate-atlantis-config/internal/atlantis"
	"github.com/devops-roast/terramate-atlantis-config/internal/discovery"
)

func TestIntegration(t *testing.T) {
	tests := []struct {
		name    string
		fixture string
		opts    atlantis.Options
	}{
		{name: "simple", fixture: "testdata/simple", opts: defaultOpts()},
		{name: "multi", fixture: "testdata/multi", opts: defaultOpts()},
		{name: "with_globals", fixture: "testdata/with_globals", opts: defaultOpts()},
		{name: "skip_stack", fixture: "testdata/skip_stack", opts: defaultOpts()},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := copyFixture(t, tt.fixture)
			initGitRepo(t, tmpDir)

			stacks, err := discovery.Discover(discovery.DiscoverOptions{RootDir: tmpDir})
			require.NoError(t, err)

			cfg := atlantis.Generate(stacks, tt.opts)
			got, err := atlantis.MarshalYAML(cfg, false)
			require.NoError(t, err)

			expectedPath, err := filepath.Abs(filepath.Join(tt.fixture, "expected.yaml"))
			require.NoError(t, err)
			expected, err := os.ReadFile(expectedPath)
			require.NoError(t, err)

			require.Equal(t, string(expected), string(got))
		})
	}
}

func defaultOpts() atlantis.Options {
	return atlantis.Options{
		Autoplan:          true,
		Parallel:          true,
		Automerge:         false,
		CreateProjectName: true,
		WhenModified:      []string{"*.tf", "*.tf.json", ".terraform.lock.hcl"},
	}
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
		if rel == "." {
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

func initGitRepo(t *testing.T, dir string) {
	t.Helper()

	cmd := exec.Command("git", "init", dir)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))

	cmd = exec.Command("git", "-C", dir, "add", ".")
	out, err = cmd.CombinedOutput()
	require.NoError(t, err, string(out))

	cmd = exec.Command("git", "-C", dir, "-c", "user.name=test", "-c", "user.email=test@test.com", "commit", "-m", "init")
	out, err = cmd.CombinedOutput()
	require.NoError(t, err, string(out))
}

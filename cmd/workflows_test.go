package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/devops-roast/terramate-atlantis-config/internal/atlantis"
)

const configFile = ".terramate-atlantis.yaml"

// parsed decodes generated YAML output.
func parsed(t *testing.T, out string) atlantis.Config {
	t.Helper()
	var cfg atlantis.Config
	require.NoError(t, yaml.Unmarshal([]byte(out), &cfg), out)
	return cfg
}

func workflowByDir(cfg atlantis.Config) map[string]string {
	m := map[string]string{}
	for _, p := range cfg.Projects {
		m[p.Dir] = p.Workflow
	}
	return m
}

func planRun(t *testing.T, cfg atlantis.Config, name string, step int) string {
	t.Helper()
	wf, ok := cfg.Workflows[name].(map[string]interface{})
	require.True(t, ok, "workflow %q missing: %#v", name, cfg.Workflows)
	steps := wf["plan"].(map[string]interface{})["steps"].([]interface{})
	return steps[step].(map[string]interface{})["run"].(string)
}

func TestGenerateWorkflowsAssignsWorkflowToProjects(t *testing.T) {
	root := newFixture(t)

	t.Run("projects without a workflow get the generated one", func(t *testing.T) {
		res := run(t, "generate", "--root", root, "--no-header", "--generate-workflows")
		require.Equal(t, 0, res.code, res.stderr)

		cfg := parsed(t, res.stdout)
		assert.Equal(t, map[string]string{"stacks/dev": "terramate", "stacks/prod": "terramate"}, workflowByDir(cfg))
		assert.Contains(t, cfg.Workflows, "terramate")
	})

	t.Run("--workflow takes precedence", func(t *testing.T) {
		res := run(t, "generate", "--root", root, "--no-header", "--generate-workflows", "--workflow", "custom")
		require.Equal(t, 0, res.code, res.stderr)

		assert.Equal(t, map[string]string{"stacks/dev": "custom", "stacks/prod": "custom"}, workflowByDir(parsed(t, res.stdout)))
	})

	t.Run("tag mapping takes precedence", func(t *testing.T) {
		res := run(t, "generate", "--root", root, "--no-header", "--generate-workflows", "--tag-workflow", "prod=production")
		require.Equal(t, 0, res.code, res.stderr)

		assert.Equal(t, map[string]string{"stacks/dev": "terramate", "stacks/prod": "production"}, workflowByDir(parsed(t, res.stdout)))
	})

	t.Run("stack global takes precedence", func(t *testing.T) {
		local := newFixture(t)
		writeFile(t, filepath.Join(local, "stacks", "dev", "globals.tm.hcl"), "globals {\n  atlantis_workflow = \"from-global\"\n}\n")

		res := run(t, "generate", "--root", local, "--no-header", "--generate-workflows")
		require.Equal(t, 0, res.code, res.stderr)

		assert.Equal(t, map[string]string{"stacks/dev": "from-global", "stacks/prod": "terramate"}, workflowByDir(parsed(t, res.stdout)))
	})

	t.Run("without --generate-workflows no workflow is assigned", func(t *testing.T) {
		res := run(t, "generate", "--root", root, "--no-header")
		require.Equal(t, 0, res.code, res.stderr)

		cfg := parsed(t, res.stdout)
		assert.Equal(t, map[string]string{"stacks/dev": "", "stacks/prod": ""}, workflowByDir(cfg))
		assert.Empty(t, cfg.Workflows)
	})
}

func TestGeneratedWorkflowCommands(t *testing.T) {
	root := newFixture(t)

	tests := []struct {
		name string
		args []string
		want string
	}{
		{"default disables git safeguards", nil, "terramate run --no-recursive --disable-safeguards=git -- terraform init -input=false"},
		{"empty value keeps safeguards", []string{"--workflow-disable-safeguards="}, "terramate run --no-recursive -- terraform init -input=false"},
		{"explicit list", []string{"--workflow-disable-safeguards", "git-untracked,git-uncommitted"}, "terramate run --no-recursive --disable-safeguards=git-untracked,git-uncommitted -- terraform init -input=false"},
		{"unwrapped", []string{"--workflow-terramate-wrap=false"}, "terraform init -input=false"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := append([]string{"generate", "--root", root, "--no-header", "--generate-workflows"}, tt.args...)
			res := run(t, args...)
			require.Equal(t, 0, res.code, res.stderr)
			assert.Equal(t, tt.want, planRun(t, parsed(t, res.stdout), "terramate", 0))
		})
	}

	t.Run("invalid safeguard keyword", func(t *testing.T) {
		res := run(t, "generate", "--root", root, "--generate-workflows", "--workflow-disable-safeguards", "gti")
		assert.Equal(t, 1, res.code)
		assert.Contains(t, res.stderr, `invalid safeguard keyword "gti"`)
	})
}

func TestWorkflowMergePrecedence(t *testing.T) {
	existing := `version: 3
workflows:
  legacy:
    plan:
      steps: [init, plan]
  terramate:
    plan:
      steps:
        - run: stale generated workflow
  shared:
    plan:
      steps:
        - run: from existing file
projects: []
`
	config := `workflows:
  shared:
    plan:
      steps:
        - run: from config
  custom:
    plan:
      steps:
        - run: custom from config
`

	setup := func(t *testing.T, cfg string) (string, string) {
		t.Helper()
		root := newFixture(t)
		out := filepath.Join(t.TempDir(), "atlantis.yaml")
		writeFile(t, out, existing)
		if cfg != "" {
			writeFile(t, filepath.Join(root, configFile), cfg)
		}
		return root, out
	}

	read := func(t *testing.T, out string) atlantis.Config {
		t.Helper()
		content, err := os.ReadFile(out)
		require.NoError(t, err)
		return parsed(t, string(content))
	}

	t.Run("preserve + generate + config", func(t *testing.T) {
		root, out := setup(t, config)

		res := generateTo(t, root, out, "--preserve-workflows", "--generate-workflows")
		require.Equal(t, 0, res.code, res.stderr)

		cfg := read(t, out)
		assert.ElementsMatch(t, []string{"legacy", "terramate", "shared", "custom"}, keys(cfg.Workflows))
		assert.Contains(t, planRun(t, cfg, "terramate", 0), "terramate run --no-recursive", "generated replaces stale preserved copy")
		assert.Equal(t, "from config", planRun(t, cfg, "shared", 0), "config overrides preserved")
		assert.Contains(t, res.stderr, `Warning: workflow "shared" from the config file overrides the one from the existing output file`)
		assert.NotContains(t, res.stderr, `"terramate"`, "regenerating a preserved generated workflow is silent")
	})

	// The existing file holds the config workflows the previous run wrote;
	// refreshing an identical copy must not warn on every run.
	t.Run("preserve + config does not warn about its own previous output", func(t *testing.T) {
		root, out := setup(t, config)

		first := generateTo(t, root, out, "--preserve-workflows")
		require.Equal(t, 0, first.code, first.stderr)
		require.Contains(t, first.stderr, `"shared"`, "the hand-written existing copy differs and is reported")

		second := generateTo(t, root, out, "--preserve-workflows")
		require.Equal(t, 0, second.code, second.stderr)
		assert.NotContains(t, second.stderr, "Warning:")
	})

	t.Run("preserve only keeps existing workflows", func(t *testing.T) {
		root, out := setup(t, "")

		res := generateTo(t, root, out, "--preserve-workflows")
		require.Equal(t, 0, res.code, res.stderr)

		cfg := read(t, out)
		assert.ElementsMatch(t, []string{"legacy", "terramate", "shared"}, keys(cfg.Workflows))
		assert.Equal(t, "stale generated workflow", planRun(t, cfg, "terramate", 0))
	})

	t.Run("preserve + config keeps both", func(t *testing.T) {
		root, out := setup(t, config)

		res := generateTo(t, root, out, "--preserve-workflows")
		require.Equal(t, 0, res.code, res.stderr)

		assert.ElementsMatch(t, []string{"legacy", "terramate", "shared", "custom"}, keys(read(t, out).Workflows))
	})

	t.Run("preserve + generate keeps both", func(t *testing.T) {
		root, out := setup(t, "")

		res := generateTo(t, root, out, "--preserve-workflows", "--generate-workflows")
		require.Equal(t, 0, res.code, res.stderr)

		cfg := read(t, out)
		assert.ElementsMatch(t, []string{"legacy", "terramate", "shared"}, keys(cfg.Workflows))
		assert.Empty(t, res.stderr[len("Generated "+out+" with 2 projects\n"):])
	})

	t.Run("generate + config keeps both and config wins on conflict", func(t *testing.T) {
		root, out := setup(t, "workflows:\n  terramate:\n    plan:\n      steps:\n        - run: my own\n  custom:\n    plan:\n      steps: [init]\n")

		res := generateTo(t, root, out, "--generate-workflows")
		require.Equal(t, 0, res.code, res.stderr)

		cfg := read(t, out)
		assert.ElementsMatch(t, []string{"terramate", "custom"}, keys(cfg.Workflows))
		assert.Equal(t, "my own", planRun(t, cfg, "terramate", 0))
		assert.Contains(t, res.stderr, `Warning: workflow "terramate" from the config file overrides the one from --generate-workflows`)
	})

	t.Run("without preserve existing workflows are dropped", func(t *testing.T) {
		root, out := setup(t, "")

		res := generateTo(t, root, out)
		require.Equal(t, 0, res.code, res.stderr)
		assert.Empty(t, read(t, out).Workflows)
	})

	t.Run("preserve with missing output file", func(t *testing.T) {
		root := newFixture(t)
		out := filepath.Join(t.TempDir(), "atlantis.yaml")

		res := generateTo(t, root, out, "--preserve-workflows", "--generate-workflows")
		require.Equal(t, 0, res.code, res.stderr)
		assert.ElementsMatch(t, []string{"terramate"}, keys(read(t, out).Workflows))
	})
}

func keys(m map[string]interface{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestConfigFileFlagEquivalents(t *testing.T) {
	t.Run("changed and changed_base_ref", func(t *testing.T) {
		root := newFixture(t)
		gitCmd(t, root, "branch", "base")
		gitCmd(t, root, "checkout", "-q", "-b", "feature")
		writeFile(t, filepath.Join(root, "stacks", "prod", "main.tf"), "terraform {}\n# change\n")
		gitCmd(t, root, "commit", "-q", "-am", "change prod")
		writeFile(t, filepath.Join(root, configFile), "changed: true\nchanged_base_ref: base\n")

		res := run(t, "generate", "--root", root, "--no-header")
		require.Equal(t, 0, res.code, res.stderr)
		assert.Contains(t, res.stdout, "dir: stacks/prod")
		assert.NotContains(t, res.stdout, "dir: stacks/dev")

		// The flag still overrides the config file.
		all := run(t, "generate", "--root", root, "--no-header", "--changed=false")
		require.Equal(t, 0, all.code, all.stderr)
		assert.Contains(t, all.stdout, "dir: stacks/dev")
	})

	t.Run("generate_workflows, wrap and safeguards", func(t *testing.T) {
		root := newFixture(t)
		writeFile(t, filepath.Join(root, configFile),
			"generate_workflows: true\nworkflow_disable_safeguards: [git-untracked]\n")

		res := run(t, "generate", "--root", root, "--no-header")
		require.Equal(t, 0, res.code, res.stderr)
		cfg := parsed(t, res.stdout)
		assert.Equal(t, "terramate run --no-recursive --disable-safeguards=git-untracked -- terraform init -input=false", planRun(t, cfg, "terramate", 0))
		assert.Equal(t, "terramate", cfg.Projects[0].Workflow)

		writeFile(t, filepath.Join(root, configFile), "generate_workflows: true\nworkflow_terramate_wrap: false\n")
		res = run(t, "generate", "--root", root, "--no-header")
		require.Equal(t, 0, res.code, res.stderr)
		assert.Equal(t, "terraform init -input=false", planRun(t, parsed(t, res.stdout), "terramate", 0))

		writeFile(t, filepath.Join(root, configFile), "generate_workflows: true\nworkflow_disable_safeguards: []\n")
		res = run(t, "generate", "--root", root, "--no-header")
		require.Equal(t, 0, res.code, res.stderr)
		assert.Equal(t, "terramate run --no-recursive -- terraform init -input=false", planRun(t, parsed(t, res.stdout), "terramate", 0))

		writeFile(t, filepath.Join(root, configFile), "generate_workflows: true\nworkflow_disable_safeguards: [nope]\n")
		res = run(t, "generate", "--root", root, "--no-header")
		assert.Equal(t, 1, res.code)
		assert.Contains(t, res.stderr, `invalid safeguard keyword "nope"`)
	})

	t.Run("preserve_workflows", func(t *testing.T) {
		root := newFixture(t)
		out := filepath.Join(t.TempDir(), "atlantis.yaml")
		writeFile(t, out, "version: 3\nworkflows:\n  legacy:\n    plan:\n      steps: [init]\nprojects: []\n")
		writeFile(t, filepath.Join(root, configFile), "preserve_workflows: true\n")

		res := generateTo(t, root, out)
		require.Equal(t, 0, res.code, res.stderr)
		content, err := os.ReadFile(out)
		require.NoError(t, err)
		assert.Contains(t, parsed(t, string(content)).Workflows, "legacy")
	})

	t.Run("no_header", func(t *testing.T) {
		root := newFixture(t)
		writeFile(t, filepath.Join(root, configFile), "no_header: true\n")

		res := run(t, "generate", "--root", root)
		require.Equal(t, 0, res.code, res.stderr)
		assert.NotContains(t, res.stdout, "# Generated by")

		withHeader := run(t, "generate", "--root", root, "--no-header=false")
		require.Equal(t, 0, withHeader.code, withHeader.stderr)
		assert.Contains(t, withHeader.stdout, "# Generated by")
	})

	t.Run("log_level", func(t *testing.T) {
		root := newFixture(t)
		gitCmd(t, root, "checkout", "-q", "-b", "feature")
		writeFile(t, filepath.Join(root, "stacks", "prod", "main.tf"), "terraform {}\n# change\n")
		gitCmd(t, root, "commit", "-q", "-am", "change prod")
		writeFile(t, filepath.Join(root, configFile), "log_level: debug\n")

		res := run(t, "generate", "--root", root, "--changed", "--no-header")
		require.Equal(t, 0, res.code, res.stderr)
		assert.NotEmpty(t, res.stderr, "log_level: debug from the config file should surface SDK logs")

		quiet := run(t, "generate", "--root", root, "--changed", "--no-header", "--log-level", "warn")
		require.Equal(t, 0, quiet.code, quiet.stderr)
		assert.Empty(t, quiet.stderr, "--log-level overrides the config file")

		writeFile(t, filepath.Join(root, configFile), "log_level: loud\n")
		bad := run(t, "generate", "--root", root)
		assert.Equal(t, 1, bad.code)
		assert.Contains(t, bad.stderr, "log_level")
	})

	t.Run("tag_requirements list", func(t *testing.T) {
		root := newFixture(t)
		writeFile(t, filepath.Join(root, configFile), "tag_requirements:\n  prod: [approved, mergeable]\n")

		res := run(t, "generate", "--root", root, "--no-header")
		require.Equal(t, 0, res.code, res.stderr)
		for _, p := range parsed(t, res.stdout).Projects {
			if p.Dir == "stacks/prod" {
				require.NotNil(t, p.ApplyRequirements)
				assert.Equal(t, []string{"approved", "mergeable"}, *p.ApplyRequirements)
			} else {
				assert.Nil(t, p.ApplyRequirements)
			}
		}
	})

	t.Run("when_modified_from_config", func(t *testing.T) {
		root := newFixture(t)
		writeFile(t, filepath.Join(root, configFile), "when_modified_from_config: true\n")

		res := run(t, "generate", "--root", root, "--no-header")
		require.Equal(t, 0, res.code, res.stderr)
		for _, p := range parsed(t, res.stdout).Projects {
			assert.Equal(t, []string{
				"*.tf", "*.tf.json", ".terraform.lock.hcl",
				"*.tm.hcl", "*.tm", "../*.tm.hcl", "../*.tm", "../../*.tm.hcl", "../../*.tm",
			}, p.Autoplan.WhenModified, p.Dir)
		}

		off := run(t, "generate", "--root", root, "--no-header", "--when-modified-from-config=false")
		require.Equal(t, 0, off.code, off.stderr)
		for _, p := range parsed(t, off.stdout).Projects {
			assert.Equal(t, []string{"*.tf", "*.tf.json", ".terraform.lock.hcl"}, p.Autoplan.WhenModified, p.Dir)
		}
	})

	t.Run("unknown key fails with the key name", func(t *testing.T) {
		root := newFixture(t)
		writeFile(t, filepath.Join(root, configFile), "chaned: true\n")

		res := run(t, "generate", "--root", root)
		assert.Equal(t, 1, res.code)
		assert.Contains(t, res.stderr, `unknown key "chaned"`)
	})
}

// Config file (and preserved) workflows are copied as written. Decoding them
// into Go values retyped unquoted scalars: Atlantis reads `value: 1.10` as the
// string "1.10", but the output said 1.1. Re-reading a float such as 1.0 as an
// int also made --preserve-workflows warn on every run, and a non-string key
// broke --format json.
func TestConfigWorkflowScalarsAreKeptAsWritten(t *testing.T) {
	root := newFixture(t)
	writeFile(t, filepath.Join(root, ".terramate-atlantis.yaml"), `workflows:
  w:
    plan:
      steps:
        - env: {name: A, value: 1.10}
        - env: {name: B, value: 0x1F}
        - env: {name: C, value: 2026-01-01}
        - env: {name: D, value: 1.0}
        - env: {name: E, value: yes}
        - env: {name: F, value: !!binary aGk=}
        - run: echo a && echo b
    1: x
`)
	output := filepath.Join(root, "atlantis.yaml")

	res := generateTo(t, root, output, "--no-header", "--preserve-workflows")
	require.Equal(t, 0, res.code, res.stderr)
	written, err := os.ReadFile(output)
	require.NoError(t, err)
	for _, want := range []string{"value: 1.10\n", "value: 0x1F\n", "value: 2026-01-01\n", "value: 1.0\n", "value: \"yes\"\n", "value: !!binary aGk=\n", "\"1\": x\n"} {
		assert.Contains(t, string(written), want)
	}

	// A second run preserves the workflow it wrote: no override warning.
	res = generateTo(t, root, output, "--no-header", "--preserve-workflows")
	require.Equal(t, 0, res.code, res.stderr)
	assert.NotContains(t, res.stderr, "overrides")

	res = run(t, "generate", "--root", root, "--format", "json")
	require.Equal(t, 0, res.code, res.stderr)
	for _, want := range []string{`"value": 1.10`, `"value": "0x1F"`, `"value": "2026-01-01"`, `"value": 1.0`, `"value": "yes"`, `"1": "x"`} {
		assert.Contains(t, res.stdout, want)
	}
}

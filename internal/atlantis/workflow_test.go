package atlantis

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func runSteps(t *testing.T, workflows map[string]interface{}, name, stage string) []string {
	t.Helper()
	workflow, ok := workflows[name].(map[string]interface{})
	require.True(t, ok, "missing workflow %q in %#v", name, workflows)
	steps := workflow[stage].(map[string]interface{})["steps"].([]interface{})
	commands := make([]string, 0, len(steps))
	for _, step := range steps {
		commands = append(commands, step.(map[string]interface{})["run"].(string))
	}
	return commands
}

func TestGenerateWorkflows(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		opts      WorkflowOptions
		wantPlan  []string
		wantApply []string
	}{
		{
			name: "wrapped with default safeguards",
			opts: WorkflowOptions{Enabled: true, WrapWithTerramate: true, DisableSafeguards: DefaultDisableSafeguards},
			wantPlan: []string{
				"terramate run --no-recursive --disable-safeguards=git -- terraform init -input=false > /dev/null",
				`terramate run --no-recursive --disable-safeguards=git -- terraform plan -input=false -out="$PLANFILE"`,
			},
			wantApply: []string{
				`terramate run --no-recursive --disable-safeguards=git -- terraform apply "$PLANFILE"`,
			},
		},
		{
			name: "wrapped with all safeguards kept",
			opts: WorkflowOptions{Enabled: true, WrapWithTerramate: true},
			wantPlan: []string{
				"terramate run --no-recursive -- terraform init -input=false > /dev/null",
				`terramate run --no-recursive -- terraform plan -input=false -out="$PLANFILE"`,
			},
			wantApply: []string{`terramate run --no-recursive -- terraform apply "$PLANFILE"`},
		},
		{
			name: "wrapped with several safeguards disabled",
			opts: WorkflowOptions{Enabled: true, WrapWithTerramate: true, DisableSafeguards: []string{"git-untracked", "outdated-code"}},
			wantPlan: []string{
				"terramate run --no-recursive --disable-safeguards=git-untracked,outdated-code -- terraform init -input=false > /dev/null",
				`terramate run --no-recursive --disable-safeguards=git-untracked,outdated-code -- terraform plan -input=false -out="$PLANFILE"`,
			},
			wantApply: []string{`terramate run --no-recursive --disable-safeguards=git-untracked,outdated-code -- terraform apply "$PLANFILE"`},
		},
		{
			name: "unwrapped ignores safeguards",
			opts: WorkflowOptions{Enabled: true, WrapWithTerramate: false, DisableSafeguards: DefaultDisableSafeguards},
			wantPlan: []string{
				"terraform init -input=false > /dev/null",
				`terraform plan -input=false -out="$PLANFILE"`,
			},
			wantApply: []string{`terraform apply "$PLANFILE"`},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			workflows := GenerateWorkflows(tt.opts)
			assert.Equal(t, tt.wantPlan, runSteps(t, workflows, GeneratedWorkflowName, "plan"))
			assert.Equal(t, tt.wantApply, runSteps(t, workflows, GeneratedWorkflowName, "apply"))
		})
	}

	t.Run("disabled returns nil", func(t *testing.T) {
		t.Parallel()
		assert.Nil(t, GenerateWorkflows(WorkflowOptions{}))
	})
}

func TestValidateSafeguards(t *testing.T) {
	t.Parallel()

	require.NoError(t, ValidateSafeguards(nil))
	require.NoError(t, ValidateSafeguards([]string{"git", "outdated-code"}))
	for _, keyword := range ValidSafeguardKeywords {
		require.NoError(t, ValidateSafeguards([]string{keyword}))
	}

	err := ValidateSafeguards([]string{"git", "gti"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), `"gti"`)
}

func TestMergeWorkflows(t *testing.T) {
	t.Parallel()

	preserved := map[string]interface{}{"legacy": "p-legacy", "terramate": "p-terramate", "shared": "p-shared"}
	generated := map[string]interface{}{"terramate": "g-terramate"}
	config := map[string]interface{}{"custom": "c-custom", "shared": "c-shared"}

	t.Run("precedence preserved < generated < config", func(t *testing.T) {
		t.Parallel()

		merged, overrides := MergeWorkflows(
			WorkflowLayer{Source: SourcePreserved, Workflows: preserved},
			WorkflowLayer{Source: SourceGenerated, Workflows: generated},
			WorkflowLayer{Source: SourceConfig, Workflows: config},
		)

		assert.Equal(t, map[string]interface{}{
			"legacy":    "p-legacy",
			"terramate": "g-terramate",
			"shared":    "c-shared",
			"custom":    "c-custom",
		}, merged)
		assert.Equal(t, []WorkflowOverride{
			{Name: "shared", Winner: SourceConfig, Replaced: SourcePreserved},
			{Name: "terramate", Winner: SourceGenerated, Replaced: SourcePreserved},
		}, overrides)
	})

	t.Run("config overrides generated", func(t *testing.T) {
		t.Parallel()

		merged, overrides := MergeWorkflows(
			WorkflowLayer{Source: SourceGenerated, Workflows: generated},
			WorkflowLayer{Source: SourceConfig, Workflows: map[string]interface{}{"terramate": "c-terramate"}},
		)

		assert.Equal(t, map[string]interface{}{"terramate": "c-terramate"}, merged)
		assert.Equal(t, []WorkflowOverride{{Name: "terramate", Winner: SourceConfig, Replaced: SourceGenerated}}, overrides)
	})

	t.Run("empty layers return nil", func(t *testing.T) {
		t.Parallel()

		merged, overrides := MergeWorkflows(
			WorkflowLayer{Source: SourcePreserved},
			WorkflowLayer{Source: SourceGenerated},
		)
		assert.Nil(t, merged)
		assert.Empty(t, overrides)
	})
}

func TestExistingWorkflows(t *testing.T) {
	t.Parallel()

	t.Run("yaml with header", func(t *testing.T) {
		t.Parallel()

		workflows, err := ExistingWorkflows([]byte(`# Generated by terramate-atlantis-config. DO NOT EDIT.
version: 3
workflows:
  custom:
    plan:
      steps:
        - init
projects:
  - dir: keep/me
`))
		require.NoError(t, err)
		assert.Contains(t, workflows, "custom")
	})

	t.Run("json", func(t *testing.T) {
		t.Parallel()

		workflows, err := ExistingWorkflows([]byte(`{"version": 3, "workflows": {"custom": {"plan": {"steps": ["init"]}}}}`))
		require.NoError(t, err)
		assert.Contains(t, workflows, "custom")
	})

	t.Run("no workflows", func(t *testing.T) {
		t.Parallel()

		workflows, err := ExistingWorkflows([]byte("version: 3\nprojects: []\n"))
		require.NoError(t, err)
		assert.Nil(t, workflows)
	})

	t.Run("workflows not a map", func(t *testing.T) {
		t.Parallel()

		_, err := ExistingWorkflows([]byte("version: 3\nworkflows: [a]\n"))
		require.Error(t, err)
	})

	t.Run("invalid yaml", func(t *testing.T) {
		t.Parallel()

		_, err := ExistingWorkflows([]byte("version: [3\n"))
		require.Error(t, err)
	})
}

// FuzzPreservedWorkflowsAreStable checks that --preserve-workflows is
// idempotent: workflows read from a file, written (YAML or JSON) and read
// back are unchanged, so a second run neither rewrites nor warns.
func FuzzPreservedWorkflowsAreStable(f *testing.F) {
	for _, seed := range []string{
		"workflows:\n  w:\n    plan:\n      steps:\n        - env: {name: A, value: 1.10}\n",
		"workflows:\n  w: {a: 0x1F, b: 2026-01-01, c: 1.0, d: yes, e: ~, f: !!binary aGk=, g: 1e3, h: -0.0}\n",
		"workflows:\n  w: &x {a: 1}\n  v: {<<: *x, b: 2}\n",
		"workflows:\n  w: {1: x, true: y, 'a b': \" c \\n\"}\n",
		"workflows: ~\n",
		"workflows: {w: [007, .inf, -.Inf, .nan, 0o17, +1, 1_000]}\n",
		"workflows:\n  w: &x [*x]\n", "workflows:\n  w: &x {<<: *x}\n", "workflows:\n  w: &x {a: {b: *x}}\n",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, content string) {
		first, err := ExistingWorkflows([]byte(content))
		if err != nil || len(first) == 0 {
			t.Skip()
		}
		for _, format := range []string{"yaml", "json"} {
			roundTrip := func(workflows map[string]interface{}) map[string]interface{} {
				cfg := Config{Version: 3, Workflows: workflows}
				var out []byte
				if format == "json" {
					out, err = MarshalJSON(cfg)
				} else {
					out, err = MarshalYAML(cfg, true)
				}
				if err != nil {
					t.Fatalf("%s: marshaling %#v: %v", format, workflows, err)
				}
				back, err := ExistingWorkflows(out)
				if err != nil {
					t.Fatalf("%s: reading back:\n%s\n%v", format, out, err)
				}
				return back
			}
			second := roundTrip(first)
			// YAML keeps every value as written; JSON may turn a scalar
			// it has no type for into a string once, then stays put.
			if format == "yaml" && !reflect.DeepEqual(first, second) {
				t.Fatalf("yaml round trip changed workflows:\n%#v\n%#v", first, second)
			}
			if third := roundTrip(second); !reflect.DeepEqual(second, third) {
				t.Fatalf("%s: second round trip changed workflows:\n%#v\n%#v", format, second, third)
			}
		}
	})
}

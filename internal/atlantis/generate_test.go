package atlantis

import (
	"bytes"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/devops-roast/terramate-atlantis-config/internal/discovery"
)

func mustGenerate(t *testing.T, stacks []discovery.StackInfo, opts Options) Config {
	t.Helper()

	cfg, err := Generate(stacks, opts)
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}
	return cfg
}

func TestGenerate(t *testing.T) {
	t.Parallel()

	falseVal := false

	testcases := []struct {
		name   string
		stacks []discovery.StackInfo
		opts   Options
		want   Config
	}{
		{
			name:   "empty stacks",
			stacks: nil,
			opts: Options{
				Autoplan:          true,
				Parallel:          true,
				Automerge:         false,
				CreateProjectName: true,
				WhenModified:      []string{"*.tf"},
			},
			want: Config{
				Version:       3,
				Automerge:     false,
				ParallelPlan:  true,
				ParallelApply: true,
				Projects:      []Project{},
			},
		},
		{
			name: "single stack with defaults",
			stacks: []discovery.StackInfo{
				{Dir: "aws/prod"},
			},
			opts: Options{
				Autoplan:          true,
				Parallel:          true,
				Automerge:         false,
				DefaultWorkflow:   "default-workflow",
				TerraformVersion:  "1.6.0",
				CreateProjectName: true,
				WhenModified:      []string{"*.tf", "*.tfvars"},
			},
			want: Config{
				Version:       3,
				Automerge:     false,
				ParallelPlan:  true,
				ParallelApply: true,
				Projects: []Project{
					{
						Name:             "aws-prod",
						Dir:              "aws/prod",
						Workflow:         "default-workflow",
						TerraformVersion: "1.6.0",
						Autoplan: Autoplan{
							Enabled:      true,
							WhenModified: []string{"*.tf", "*.tfvars"},
						},
					},
				},
			},
		},
		{
			name: "multiple stacks sorted by dir",
			stacks: []discovery.StackInfo{
				{Dir: "z/prod"},
				{Dir: "a/prod"},
			},
			opts: Options{
				Autoplan:          true,
				Parallel:          true,
				CreateProjectName: true,
				WhenModified:      []string{"*.tf"},
			},
			want: Config{
				Version:       3,
				ParallelPlan:  true,
				ParallelApply: true,
				Projects: []Project{
					{
						Name: "a-prod",
						Dir:  "a/prod",
						Autoplan: Autoplan{
							Enabled:      true,
							WhenModified: []string{"*.tf"},
						},
					},
					{
						Name: "z-prod",
						Dir:  "z/prod",
						Autoplan: Autoplan{
							Enabled:      true,
							WhenModified: []string{"*.tf"},
						},
					},
				},
			},
		},
		{
			name: "skip true filtered out",
			stacks: []discovery.StackInfo{
				{Dir: "aws/dev", Skip: true},
				{Dir: "aws/prod"},
			},
			opts: Options{
				Autoplan:          true,
				Parallel:          true,
				CreateProjectName: true,
				WhenModified:      []string{"*.tf"},
			},
			want: Config{
				Version:       3,
				ParallelPlan:  true,
				ParallelApply: true,
				Projects: []Project{
					{
						Name: "aws-prod",
						Dir:  "aws/prod",
						Autoplan: Autoplan{
							Enabled:      true,
							WhenModified: []string{"*.tf"},
						},
					},
				},
			},
		},
		{
			name: "custom workflow override",
			stacks: []discovery.StackInfo{
				{Dir: "aws/prod", Workflow: "custom"},
			},
			opts: Options{
				Autoplan:          true,
				Parallel:          true,
				DefaultWorkflow:   "default-workflow",
				CreateProjectName: true,
				WhenModified:      []string{"*.tf"},
			},
			want: Config{
				Version:       3,
				ParallelPlan:  true,
				ParallelApply: true,
				Projects: []Project{
					{
						Name:     "aws-prod",
						Dir:      "aws/prod",
						Workflow: "custom",
						Autoplan: Autoplan{
							Enabled:      true,
							WhenModified: []string{"*.tf"},
						},
					},
				},
			},
		},
		{
			name: "custom when modified override",
			stacks: []discovery.StackInfo{
				{Dir: "aws/prod", WhenModified: []string{"main.tf", "vars.tf"}},
			},
			opts: Options{
				Autoplan:          true,
				Parallel:          true,
				CreateProjectName: true,
				WhenModified:      []string{"*.tf"},
			},
			want: Config{
				Version:       3,
				ParallelPlan:  true,
				ParallelApply: true,
				Projects: []Project{
					{
						Name: "aws-prod",
						Dir:  "aws/prod",
						Autoplan: Autoplan{
							Enabled:      true,
							WhenModified: []string{"main.tf", "vars.tf"},
						},
					},
				},
			},
		},
		{
			name: "extra deps merged with defaults",
			stacks: []discovery.StackInfo{
				{Dir: "aws/prod", ExtraDeps: []string{"../modules/**/*.tf"}},
			},
			opts: Options{
				Autoplan:          true,
				Parallel:          true,
				CreateProjectName: true,
				WhenModified:      []string{"*.tf", "*.tfvars"},
			},
			want: Config{
				Version:       3,
				ParallelPlan:  true,
				ParallelApply: true,
				Projects: []Project{
					{
						Name: "aws-prod",
						Dir:  "aws/prod",
						Autoplan: Autoplan{
							Enabled:      true,
							WhenModified: []string{"*.tf", "*.tfvars", "../modules/**/*.tf"},
						},
					},
				},
			},
		},
		{
			name: "autoplan override false",
			stacks: []discovery.StackInfo{
				{Dir: "aws/prod", AutoplanEnabled: &falseVal},
			},
			opts: Options{
				Autoplan:          true,
				Parallel:          true,
				CreateProjectName: true,
				WhenModified:      []string{"*.tf"},
			},
			want: Config{
				Version:       3,
				ParallelPlan:  true,
				ParallelApply: true,
				Projects: []Project{
					{
						Name: "aws-prod",
						Dir:  "aws/prod",
						Autoplan: Autoplan{
							Enabled:      false,
							WhenModified: []string{"*.tf"},
						},
					},
				},
			},
		},
		{
			name: "terraform version override",
			stacks: []discovery.StackInfo{
				{Dir: "aws/prod", TerraformVersion: "1.5.0"},
			},
			opts: Options{
				Autoplan:          true,
				Parallel:          true,
				TerraformVersion:  "1.7.0",
				CreateProjectName: true,
				WhenModified:      []string{"*.tf"},
			},
			want: Config{
				Version:       3,
				ParallelPlan:  true,
				ParallelApply: true,
				Projects: []Project{
					{
						Name:             "aws-prod",
						Dir:              "aws/prod",
						TerraformVersion: "1.5.0",
						Autoplan: Autoplan{
							Enabled:      true,
							WhenModified: []string{"*.tf"},
						},
					},
				},
			},
		},
		{
			name: "create project name true sanitizes path",
			stacks: []discovery.StackInfo{
				{Dir: "/aws/prod/vpc"},
			},
			opts: Options{
				Autoplan:          true,
				Parallel:          true,
				CreateProjectName: true,
				WhenModified:      []string{"*.tf"},
			},
			want: Config{
				Version:       3,
				ParallelPlan:  true,
				ParallelApply: true,
				Projects: []Project{
					{
						Name: "aws-prod-vpc",
						Dir:  "aws/prod/vpc",
						Autoplan: Autoplan{
							Enabled:      true,
							WhenModified: []string{"*.tf"},
						},
					},
				},
			},
		},
		{
			name: "create project name false omits name",
			stacks: []discovery.StackInfo{
				{Dir: "aws/prod/vpc"},
			},
			opts: Options{
				Autoplan:          true,
				Parallel:          true,
				CreateProjectName: false,
				WhenModified:      []string{"*.tf"},
			},
			want: Config{
				Version:       3,
				ParallelPlan:  true,
				ParallelApply: true,
				Projects: []Project{
					{
						Dir: "aws/prod/vpc",
						Autoplan: Autoplan{
							Enabled:      true,
							WhenModified: []string{"*.tf"},
						},
					},
				},
			},
		},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := mustGenerate(t, tc.stacks, tc.opts)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("unexpected config: got %#v, want %#v", got, tc.want)
			}
		})
	}
}

func TestMarshalYAML(t *testing.T) {
	t.Parallel()

	cfg := Config{
		Version:       3,
		Automerge:     false,
		ParallelPlan:  true,
		ParallelApply: true,
		Projects: []Project{
			{
				Dir:              "aws/prod",
				Workflow:         "custom",
				TerraformVersion: "1.5.0",
				Autoplan: Autoplan{
					Enabled:      true,
					WhenModified: []string{"*.tf", "../modules/**/*.tf"},
				},
			},
		},
	}

	got, err := MarshalYAML(cfg, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := `version: 3
automerge: false
parallel_plan: true
parallel_apply: true
projects:
  - dir: aws/prod
    workflow: custom
    terraform_version: 1.5.0
    autoplan:
      enabled: true
      when_modified:
        - '*.tf'
        - ../modules/**/*.tf
`

	if string(got) != want {
		t.Fatalf("unexpected yaml:\n%s\nwant:\n%s", string(got), want)
	}
}

func TestMarshalYAMLWithHeader(t *testing.T) {
	t.Parallel()

	cfg := Config{
		Version:       3,
		ParallelPlan:  true,
		ParallelApply: true,
		Projects: []Project{
			{Dir: "aws/prod", Autoplan: Autoplan{Enabled: true}},
		},
	}

	got, err := MarshalYAML(cfg, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wantPrefix := "# Generated by terramate-atlantis-config. DO NOT EDIT.\n# Re-generate with: terramate-atlantis-config generate\n"
	if len(got) < len(wantPrefix) || string(got[:len(wantPrefix)]) != wantPrefix {
		t.Fatalf("missing header: got %q", string(got))
	}
}

func TestMarshalJSON(t *testing.T) {
	t.Parallel()

	cfg := Config{
		Version:       3,
		Automerge:     false,
		ParallelPlan:  true,
		ParallelApply: true,
		Projects: []Project{
			{
				Name: "aws-prod",
				Dir:  "aws/prod",
				Autoplan: Autoplan{
					Enabled:      true,
					WhenModified: []string{"*.tf"},
				},
			},
		},
	}

	got, err := MarshalJSON(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := `{
  "version": 3,
  "automerge": false,
  "parallel_plan": true,
  "parallel_apply": true,
  "projects": [
    {
      "name": "aws-prod",
      "dir": "aws/prod",
      "autoplan": {
        "enabled": true,
        "when_modified": [
          "*.tf"
        ]
      }
    }
  ]
}
`

	if string(got) != want {
		t.Fatalf("unexpected json:\n%s\nwant:\n%s", string(got), want)
	}
}

func TestGenerateExecutionOrderGroupsAndDependsOn(t *testing.T) {
	t.Parallel()

	stacks := []discovery.StackInfo{
		{Dir: "env/a", After: []string{"../b"}, AfterDirs: []string{"env/b"}},
		{Dir: "env/b"},
		{Dir: "env/c", Before: []string{"../b"}, BeforeDirs: []string{"env/b"}},
	}

	cfg := mustGenerate(t, stacks, Options{
		Autoplan:             true,
		Parallel:             true,
		WhenModified:         []string{"*.tf"},
		CreateProjectName:    true,
		ExecutionOrderGroups: true,
		DependsOn:            true,
	})

	if len(cfg.Projects) != 3 {
		t.Fatalf("expected 3 projects, got %d", len(cfg.Projects))
	}

	projects := map[string]Project{}
	for _, project := range cfg.Projects {
		projects[project.Dir] = project
	}

	if projects["env/c"].ExecutionOrderGroup != 0 {
		t.Fatalf("env/c group: got %d, want 0", projects["env/c"].ExecutionOrderGroup)
	}

	if projects["env/b"].ExecutionOrderGroup != 1 {
		t.Fatalf("env/b group: got %d, want 1", projects["env/b"].ExecutionOrderGroup)
	}

	if projects["env/a"].ExecutionOrderGroup != 2 {
		t.Fatalf("env/a group: got %d, want 2", projects["env/a"].ExecutionOrderGroup)
	}

	// depends_on must reference project names, not dirs: Atlantis matches
	// depends_on entries against project names.
	if !reflect.DeepEqual(projects["env/a"].DependsOn, []string{"b"}) {
		t.Fatalf("env/a depends_on: got %#v", projects["env/a"].DependsOn)
	}

	if !reflect.DeepEqual(projects["env/b"].DependsOn, []string{"c"}) {
		t.Fatalf("env/b depends_on: got %#v", projects["env/b"].DependsOn)
	}
}

func TestGenerateApplyRequirementsPrecedence(t *testing.T) {
	t.Parallel()

	stacks := []discovery.StackInfo{
		{Dir: "stack/a", Tags: []string{"prod"}, ApplyRequirements: []string{"approved"}},
		{Dir: "stack/b", Tags: []string{"prod"}},
		{Dir: "stack/c"},
		{Dir: "stack/d", Tags: []string{"prod"}, ApplyRequirements: []string{}},
	}

	cfg := mustGenerate(t, stacks, Options{
		Autoplan:          true,
		Parallel:          true,
		WhenModified:      []string{"*.tf"},
		ApplyRequirements: []string{"mergeable"},
		TagRequirements: map[string][]string{
			"prod": {"policy_check"},
		},
	})

	projects := map[string]Project{}
	for _, project := range cfg.Projects {
		projects[project.Dir] = project
	}

	if got := projects["stack/a"].ApplyRequirements; got == nil || !reflect.DeepEqual(*got, []string{"approved"}) {
		t.Fatalf("stack/a apply_requirements: got %#v", got)
	}

	if got := projects["stack/b"].ApplyRequirements; got == nil || !reflect.DeepEqual(*got, []string{"policy_check"}) {
		t.Fatalf("stack/b apply_requirements: got %#v", got)
	}

	if got := projects["stack/c"].ApplyRequirements; got == nil || !reflect.DeepEqual(*got, []string{"mergeable"}) {
		t.Fatalf("stack/c apply_requirements: got %#v", got)
	}

	// An explicit empty list beats tag and default requirements.
	if got := projects["stack/d"].ApplyRequirements; got == nil || len(*got) != 0 {
		t.Fatalf("stack/d apply_requirements: want explicit empty list, got %#v", got)
	}
}

func TestApplyRequirementsMarshaling(t *testing.T) {
	t.Parallel()

	cfg, err := Generate([]discovery.StackInfo{
		{Dir: "empty", ApplyRequirements: []string{}},
		{Dir: "unset"},
	}, Options{})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	// Empty must be written (Atlantis reads it as "no requirements"); unset
	// must be omitted (Atlantis then uses the server-side requirements).
	yamlOut, err := MarshalYAML(cfg, false)
	if err != nil {
		t.Fatalf("marshal yaml: %v", err)
	}
	if n := strings.Count(string(yamlOut), "apply_requirements: []"); n != 1 {
		t.Fatalf("want exactly one `apply_requirements: []` in YAML, got %d:\n%s", n, yamlOut)
	}

	jsonOut, err := MarshalJSON(cfg)
	if err != nil {
		t.Fatalf("marshal json: %v", err)
	}
	if n := strings.Count(string(jsonOut), `"apply_requirements": []`); n != 1 {
		t.Fatalf("want exactly one empty apply_requirements in JSON, got %d:\n%s", n, jsonOut)
	}
}

func TestGenerateCreateWorkspace(t *testing.T) {
	t.Parallel()

	cfg := mustGenerate(t, []discovery.StackInfo{{Dir: "aws/prod"}}, Options{
		Autoplan:        true,
		Parallel:        true,
		WhenModified:    []string{"*.tf"},
		CreateWorkspace: true,
	})

	if len(cfg.Projects) != 1 {
		t.Fatalf("expected 1 project, got %d", len(cfg.Projects))
	}

	if cfg.Projects[0].Workspace != "aws-prod" {
		t.Fatalf("workspace: got %q, want %q", cfg.Projects[0].Workspace, "aws-prod")
	}
}

func TestGenerateTagWorkflowPrecedence(t *testing.T) {
	t.Parallel()

	stacks := []discovery.StackInfo{
		{Dir: "stack/a", Tags: []string{"prod"}, Workflow: "stack-workflow"},
		{Dir: "stack/b", Tags: []string{"prod"}},
		{Dir: "stack/c"},
	}

	cfg := mustGenerate(t, stacks, Options{
		Autoplan:        true,
		Parallel:        true,
		WhenModified:    []string{"*.tf"},
		DefaultWorkflow: "default-workflow",
		TagWorkflows: map[string]string{
			"prod": "prod-workflow",
		},
	})

	projects := map[string]Project{}
	for _, project := range cfg.Projects {
		projects[project.Dir] = project
	}

	if projects["stack/a"].Workflow != "stack-workflow" {
		t.Fatalf("stack/a workflow: got %q", projects["stack/a"].Workflow)
	}

	if projects["stack/b"].Workflow != "prod-workflow" {
		t.Fatalf("stack/b workflow: got %q", projects["stack/b"].Workflow)
	}

	if projects["stack/c"].Workflow != "default-workflow" {
		t.Fatalf("stack/c workflow: got %q", projects["stack/c"].Workflow)
	}
}

func TestProjectNameStrategyAutoStrip(t *testing.T) {
	t.Parallel()

	stacks := []discovery.StackInfo{
		{Dir: "stacks/aws/prod/vpc"},
		{Dir: "stacks/aws/prod/rds"},
		{Dir: "stacks/aws/staging/vpc"},
	}

	cfg := mustGenerate(t, stacks, Options{
		Autoplan:          true,
		Parallel:          true,
		CreateProjectName: true,
		WhenModified:      []string{"*.tf"},
	})

	names := projectNames(cfg)
	// common prefix is "stacks/aws" so it gets stripped
	wantNames := []string{"prod-rds", "prod-vpc", "staging-vpc"}
	if !reflect.DeepEqual(names, wantNames) {
		t.Fatalf("auto-strip names: got %v, want %v", names, wantNames)
	}
}

func TestProjectNameStrategyAutoStripSingleStack(t *testing.T) {
	t.Parallel()

	cfg := mustGenerate(t, []discovery.StackInfo{{Dir: "stacks/aws/prod/vpc"}}, Options{
		Autoplan:          true,
		Parallel:          true,
		CreateProjectName: true,
		WhenModified:      []string{"*.tf"},
	})

	// A lone stack is its own common prefix; stripping must never leave an
	// empty name, so it keeps its full path.
	if cfg.Projects[0].Name != "stacks-aws-prod-vpc" {
		t.Fatalf("single stack auto-strip should keep full path: got %q", cfg.Projects[0].Name)
	}
}

func TestProjectNameStrategyFull(t *testing.T) {
	t.Parallel()

	stacks := []discovery.StackInfo{
		{Dir: "stacks/aws/prod/vpc"},
		{Dir: "stacks/aws/staging/vpc"},
	}

	cfg := mustGenerate(t, stacks, Options{
		Autoplan:            true,
		Parallel:            true,
		CreateProjectName:   true,
		ProjectNameStrategy: NameStrategyFull,
		WhenModified:        []string{"*.tf"},
	})

	names := projectNames(cfg)
	wantNames := []string{"stacks-aws-prod-vpc", "stacks-aws-staging-vpc"}
	if !reflect.DeepEqual(names, wantNames) {
		t.Fatalf("full names: got %v, want %v", names, wantNames)
	}
}

func TestProjectNameStrategyLastN(t *testing.T) {
	t.Parallel()

	stacks := []discovery.StackInfo{
		{Dir: "stacks/aws/prod/eu-west-1/vpc"},
		{Dir: "stacks/aws/prod/eu-west-1/rds"},
		{Dir: "stacks/aws/staging/us-east-1/vpc"},
	}

	cfg := mustGenerate(t, stacks, Options{
		Autoplan:            true,
		Parallel:            true,
		CreateProjectName:   true,
		ProjectNameStrategy: NameStrategyLastN,
		ProjectNameDepth:    2,
		WhenModified:        []string{"*.tf"},
	})

	names := projectNames(cfg)
	wantNames := []string{"eu-west-1-rds", "eu-west-1-vpc", "us-east-1-vpc"}
	if !reflect.DeepEqual(names, wantNames) {
		t.Fatalf("last-n names: got %v, want %v", names, wantNames)
	}
}

func TestProjectNameStrategyLastNDefaultDepth(t *testing.T) {
	t.Parallel()

	stacks := []discovery.StackInfo{
		{Dir: "a/b/c/d/e"},
	}

	cfg := mustGenerate(t, stacks, Options{
		Autoplan:            true,
		Parallel:            true,
		CreateProjectName:   true,
		ProjectNameStrategy: NameStrategyLastN,
		WhenModified:        []string{"*.tf"},
	})

	if cfg.Projects[0].Name != "c-d-e" {
		t.Fatalf("default depth=3: got %q, want %q", cfg.Projects[0].Name, "c-d-e")
	}
}

func TestProjectNameStrategyStackName(t *testing.T) {
	t.Parallel()

	stacks := []discovery.StackInfo{
		{Dir: "stacks/aws/prod/eu-west-1/foundation", Name: "foundation"},
		{Dir: "stacks/aws/prod/eu-west-1/rds", Name: "rds"},
	}

	cfg := mustGenerate(t, stacks, Options{
		Autoplan:            true,
		Parallel:            true,
		CreateProjectName:   true,
		ProjectNameStrategy: NameStrategyStackName,
		WhenModified:        []string{"*.tf"},
	})

	names := projectNames(cfg)
	wantNames := []string{"aws-prod-eu-west-1-foundation", "aws-prod-eu-west-1-rds"}
	if !reflect.DeepEqual(names, wantNames) {
		t.Fatalf("stack-name names: got %v, want %v", names, wantNames)
	}
}

func TestProjectNameStrategyStackNameFallsBackToBasename(t *testing.T) {
	t.Parallel()

	stacks := []discovery.StackInfo{
		{Dir: "stacks/vpc"},
	}

	cfg := mustGenerate(t, stacks, Options{
		Autoplan:            true,
		Parallel:            true,
		CreateProjectName:   true,
		ProjectNameStrategy: NameStrategyStackName,
		WhenModified:        []string{"*.tf"},
	})

	if cfg.Projects[0].Name != "stacks-vpc" {
		t.Fatalf("stack-name without Name field: got %q, want %q", cfg.Projects[0].Name, "stacks-vpc")
	}
}

func TestProjectNameDisambiguation(t *testing.T) {
	t.Parallel()

	stacks := []discovery.StackInfo{
		{Dir: "stacks/aws/dev/eu-west-1/billing", Name: "billing"},
		{Dir: "stacks/aws/prod/eu-west-1/billing", Name: "billing"},
		{Dir: "stacks/aws/staging/eu-west-1/billing", Name: "billing"},
	}

	cfg := mustGenerate(t, stacks, Options{
		Autoplan:            true,
		Parallel:            true,
		CreateProjectName:   true,
		ProjectNameStrategy: NameStrategyLastN,
		ProjectNameDepth:    1,
		WhenModified:        []string{"*.tf"},
	})

	names := projectNames(cfg)
	seen := make(map[string]bool)
	for _, name := range names {
		if seen[name] {
			t.Fatalf("duplicate name %q found in %v", name, names)
		}
		seen[name] = true
	}

	if len(names) != 3 {
		t.Fatalf("expected 3 unique names, got %d: %v", len(names), names)
	}
}

func TestProjectNameDisambiguationAddsMinimalContext(t *testing.T) {
	t.Parallel()

	stacks := []discovery.StackInfo{
		{Dir: "stacks/aws/dev/eu-west-1/vpc"},
		{Dir: "stacks/aws/prod/eu-west-1/vpc"},
	}

	cfg := mustGenerate(t, stacks, Options{
		Autoplan:            true,
		Parallel:            true,
		CreateProjectName:   true,
		ProjectNameStrategy: NameStrategyLastN,
		ProjectNameDepth:    1,
		WhenModified:        []string{"*.tf"},
	})

	names := projectNames(cfg)
	for _, name := range names {
		if name == "stacks-aws-dev-eu-west-1-vpc" || name == "stacks-aws-prod-eu-west-1-vpc" {
			t.Fatalf("disambiguation used full path instead of minimal context: %q", name)
		}
	}

	wantNames := []string{"dev-eu-west-1-vpc", "prod-eu-west-1-vpc"}
	if !reflect.DeepEqual(names, wantNames) {
		t.Fatalf("minimal disambiguation: got %v, want %v", names, wantNames)
	}
}

func TestProjectNameExplicitPrefix(t *testing.T) {
	t.Parallel()

	stacks := []discovery.StackInfo{
		{Dir: "stacks/aws/prod/vpc"},
		{Dir: "stacks/aws/prod/rds"},
	}

	cfg := mustGenerate(t, stacks, Options{
		Autoplan:          true,
		Parallel:          true,
		CreateProjectName: true,
		ProjectNamePrefix: "stacks/aws",
		WhenModified:      []string{"*.tf"},
	})

	names := projectNames(cfg)
	wantNames := []string{"prod-rds", "prod-vpc"}
	if !reflect.DeepEqual(names, wantNames) {
		t.Fatalf("explicit prefix: got %v, want %v", names, wantNames)
	}
}

func TestProjectNameNoCommonPrefix(t *testing.T) {
	t.Parallel()

	stacks := []discovery.StackInfo{
		{Dir: "aws/prod/vpc"},
		{Dir: "gcp/prod/network"},
	}

	cfg := mustGenerate(t, stacks, Options{
		Autoplan:          true,
		Parallel:          true,
		CreateProjectName: true,
		WhenModified:      []string{"*.tf"},
	})

	names := projectNames(cfg)
	wantNames := []string{"aws-prod-vpc", "gcp-prod-network"}
	if !reflect.DeepEqual(names, wantNames) {
		t.Fatalf("no common prefix: got %v, want %v", names, wantNames)
	}
}

func TestProjectNameDeepHierarchyDisambiguation(t *testing.T) {
	t.Parallel()

	stacks := []discovery.StackInfo{
		{Dir: "stacks/aws/iris-dev/eu-west-1/db/postgres-1/billing"},
		{Dir: "stacks/aws/iris-prod/eu-west-1/db/postgres-1/billing"},
		{Dir: "stacks/aws/iris-staging/eu-west-1/db/postgres-1/billing"},
	}

	cfg := mustGenerate(t, stacks, Options{
		Autoplan:            true,
		Parallel:            true,
		CreateProjectName:   true,
		ProjectNameStrategy: NameStrategyLastN,
		ProjectNameDepth:    2,
		WhenModified:        []string{"*.tf"},
	})

	names := projectNames(cfg)
	seen := make(map[string]bool)
	for _, name := range names {
		if seen[name] {
			t.Fatalf("duplicate name %q in deep hierarchy: %v", name, names)
		}
		seen[name] = true
	}

	for _, name := range names {
		if strings.HasPrefix(name, "stacks-") {
			t.Fatalf("disambiguation should not include full path prefix: %q", name)
		}
	}
}

func projectNames(cfg Config) []string {
	names := make([]string, 0, len(cfg.Projects))
	for _, p := range cfg.Projects {
		names = append(names, p.Name)
	}
	return names
}

func TestGenerateSortBy(t *testing.T) {
	t.Parallel()

	stacks := []discovery.StackInfo{
		{Dir: "z/prod", AfterDirs: []string{"a/prod"}},
		{Dir: "a/prod", AfterDirs: []string{"m/prod"}},
		{Dir: "m/prod"},
	}

	testcases := []struct {
		name    string
		sortBy  string
		wantDir []string
	}{
		{
			name:    "sort by dir",
			sortBy:  "dir",
			wantDir: []string{"a/prod", "m/prod", "z/prod"},
		},
		{
			name:    "sort by name",
			sortBy:  "name",
			wantDir: []string{"a/prod", "m/prod", "z/prod"},
		},
		{
			name:    "sort by execution order group",
			sortBy:  "execution_order_group",
			wantDir: []string{"m/prod", "a/prod", "z/prod"},
		},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			cfg := mustGenerate(t, stacks, Options{
				Autoplan:             true,
				Parallel:             true,
				WhenModified:         []string{"*.tf"},
				CreateProjectName:    true,
				ExecutionOrderGroups: true,
				SortBy:               tc.sortBy,
			})

			got := make([]string, 0, len(cfg.Projects))
			for _, project := range cfg.Projects {
				got = append(got, project.Dir)
			}

			if !reflect.DeepEqual(got, tc.wantDir) {
				t.Fatalf("unexpected project order: got %#v, want %#v", got, tc.wantDir)
			}
		})
	}
}

func TestGenerateDependsOnUsesProjectNames(t *testing.T) {
	t.Parallel()

	stacks := []discovery.StackInfo{
		{Dir: "stacks/aws/acme-production/eu-west-1/dns"},
		{Dir: "stacks/aws/acme-production/eu-west-1/app", AfterDirs: []string{"stacks/aws/acme-production/eu-west-1/dns"}},
		{Dir: "stacks/aws/acme-production/eu-west-1/vpc", BeforeDirs: []string{"stacks/aws/acme-production/eu-west-1/app"}},
	}

	for _, strategy := range []string{NameStrategyAutoStrip, NameStrategyStackName, NameStrategyLastN, NameStrategyFull} {
		cfg := mustGenerate(t, stacks, Options{
			CreateProjectName:   true,
			DependsOn:           true,
			ProjectNameStrategy: strategy,
		})

		names := namesByDirOf(cfg)
		var app Project
		for _, p := range cfg.Projects {
			if p.Dir == "stacks/aws/acme-production/eu-west-1/app" {
				app = p
			}
		}

		want := []string{names["stacks/aws/acme-production/eu-west-1/dns"], names["stacks/aws/acme-production/eu-west-1/vpc"]}
		sort.Strings(want)
		if !reflect.DeepEqual(app.DependsOn, want) {
			t.Fatalf("%s: depends_on: got %v, want %v (project names)", strategy, app.DependsOn, want)
		}
	}
}

func TestGenerateDependsOnRequiresProjectNames(t *testing.T) {
	t.Parallel()

	_, err := Generate([]discovery.StackInfo{{Dir: "a"}}, Options{DependsOn: true})
	if !errors.Is(err, ErrDependsOnRequiresNames) {
		t.Fatalf("expected ErrDependsOnRequiresNames, got %v", err)
	}

	// Execution order groups don't reference names, so they still work.
	if _, err := Generate([]discovery.StackInfo{{Dir: "a"}}, Options{ExecutionOrderGroups: true}); err != nil {
		t.Fatalf("execution order groups without names: %v", err)
	}
}

func TestGenerateDropsDependenciesOnExcludedStacks(t *testing.T) {
	t.Parallel()

	stacks := []discovery.StackInfo{
		{Dir: "a", AfterDirs: []string{"b", "c", "missing"}},
		{Dir: "b", Skip: true},
		{Dir: "c"},
	}

	cfg := mustGenerate(t, stacks, Options{CreateProjectName: true, DependsOn: true, ExecutionOrderGroups: true})
	byDir := map[string]Project{}
	for _, p := range cfg.Projects {
		byDir[p.Dir] = p
	}

	if !reflect.DeepEqual(byDir["a"].DependsOn, []string{"c"}) {
		t.Fatalf("depends_on: got %v, want [c]", byDir["a"].DependsOn)
	}
	if byDir["a"].ExecutionOrderGroup != 1 {
		t.Fatalf("execution_order_group: got %d, want 1", byDir["a"].ExecutionOrderGroup)
	}
}

func TestGenerateWarnsOnDependencyCycle(t *testing.T) {
	t.Parallel()

	stacks := []discovery.StackInfo{
		{Dir: "a", AfterDirs: []string{"b"}},
		{Dir: "b", AfterDirs: []string{"a"}},
		{Dir: "c", AfterDirs: []string{"d"}},
		{Dir: "d"},
	}

	var warnings []string
	cfg := mustGenerate(t, stacks, Options{
		CreateProjectName:    true,
		ExecutionOrderGroups: true,
		Warnf: func(format string, args ...any) {
			warnings = append(warnings, fmt.Sprintf(format, args...))
		},
	})

	if len(warnings) != 1 {
		t.Fatalf("expected 1 warning, got %v", warnings)
	}
	if !strings.Contains(warnings[0], "dependency cycle") || !strings.Contains(warnings[0], "a, b") {
		t.Fatalf("unexpected warning: %q", warnings[0])
	}

	groups := map[string]int{}
	for _, p := range cfg.Projects {
		groups[p.Dir] = p.ExecutionOrderGroup
	}
	if !reflect.DeepEqual(groups, map[string]int{"a": 0, "b": 0, "c": 1, "d": 0}) {
		t.Fatalf("groups: got %v", groups)
	}

	// No cycle, no warning; a nil Warnf is safe.
	testNoCycleNoWarning(t, stacks)
}

// A stack that depends on a cycle is not itself on the cycle: it must still
// run after all of its dependencies, including the cycle and any ordinary
// chain, and must not be reported as cyclic.
func TestExecutionOrderGroupsAboveCycle(t *testing.T) {
	t.Parallel()

	stacks := []discovery.StackInfo{
		{Dir: "a", AfterDirs: []string{"b", "base"}},
		{Dir: "b", AfterDirs: []string{"a"}},
		{Dir: "base"},
		{Dir: "d", AfterDirs: []string{"a", "n2"}},
		{Dir: "n1"},
		{Dir: "n2", AfterDirs: []string{"n1"}},
	}

	var warnings []string
	cfg := mustGenerate(t, stacks, Options{
		CreateProjectName:    true,
		ExecutionOrderGroups: true,
		Warnf: func(format string, args ...any) {
			warnings = append(warnings, fmt.Sprintf(format, args...))
		},
	})

	groups := map[string]int{}
	for _, p := range cfg.Projects {
		groups[p.Dir] = p.ExecutionOrderGroup
	}
	want := map[string]int{"base": 0, "a": 1, "b": 1, "n1": 0, "n2": 1, "d": 2}
	if !reflect.DeepEqual(groups, want) {
		t.Fatalf("groups: got %v, want %v", groups, want)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "stacks a, b;") {
		t.Fatalf("expected one warning naming only a and b, got %v", warnings)
	}
}

func testNoCycleNoWarning(t *testing.T, stacks []discovery.StackInfo) {
	t.Helper()
	var warnings []string
	mustGenerate(t, stacks[2:], Options{ExecutionOrderGroups: true, Warnf: func(string, ...any) { warnings = append(warnings, "x") }})
	mustGenerate(t, stacks, Options{ExecutionOrderGroups: true})
	if len(warnings) != 0 {
		t.Fatalf("unexpected warnings: %v", warnings)
	}
}

func TestGenerateEmptyWhenModifiedOverride(t *testing.T) {
	t.Parallel()

	cfg, err := Generate([]discovery.StackInfo{
		{Dir: "app", WhenModified: []string{}},
	}, Options{WhenModified: []string{"*.tf"}})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	got := cfg.Projects[0].Autoplan.WhenModified
	if got == nil || len(got) != 0 {
		t.Fatalf("empty when_modified override must stay an empty list, got %#v", got)
	}

	out, err := MarshalJSON(cfg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(out), `"when_modified": []`) {
		t.Fatalf("JSON must render an empty list, not null:\n%s", out)
	}
}

func TestMarshalOutputEndsWithSingleNewline(t *testing.T) {
	t.Parallel()

	cfg := mustGenerate(t, []discovery.StackInfo{{Dir: "a"}}, Options{CreateProjectName: true})
	jsonOut, err := MarshalJSON(cfg)
	if err != nil {
		t.Fatal(err)
	}
	yamlOut, err := MarshalYAML(cfg, true)
	if err != nil {
		t.Fatal(err)
	}
	for name, out := range map[string][]byte{"json": jsonOut, "yaml": yamlOut} {
		if !bytes.HasSuffix(out, []byte("\n")) || bytes.HasSuffix(out, []byte("\n\n")) {
			t.Errorf("%s output must end with exactly one newline: %q", name, out[max(0, len(out)-10):])
		}
	}
}

// Ordering is followed through stacks that are not generated, as
// `terramate run` does: a runs after m and m after z, so with m filtered
// out (only in AllStacks) or skipped, a still runs after z.
func TestDependenciesFollowedThroughExcludedStacks(t *testing.T) {
	t.Parallel()

	all := []discovery.StackInfo{
		{Dir: "a", AfterDirs: []string{"m"}},
		{Dir: "m", AfterDirs: []string{"z"}},
		{Dir: "n", BeforeDirs: []string{"a"}},
		{Dir: "y", BeforeDirs: []string{"n"}},
		{Dir: "z"},
	}
	check := func(name string, cfg Config) {
		t.Helper()
		byDir := map[string]Project{}
		for _, p := range cfg.Projects {
			byDir[p.Dir] = p
		}
		if got := byDir["a"].DependsOn; !reflect.DeepEqual(got, []string{"y", "z"}) {
			t.Fatalf("%s: depends_on of a: got %v, want [y z]", name, got)
		}
		if byDir["a"].ExecutionOrderGroup != 1 || byDir["z"].ExecutionOrderGroup != 0 {
			t.Fatalf("%s: groups: a=%d z=%d", name, byDir["a"].ExecutionOrderGroup, byDir["z"].ExecutionOrderGroup)
		}
	}
	opts := Options{CreateProjectName: true, DependsOn: true, ExecutionOrderGroups: true}

	// m and n filtered out: only known through AllStacks.
	filtered := opts
	filtered.AllStacks = all
	check("filtered", mustGenerate(t, []discovery.StackInfo{all[0], all[3], all[4]}, filtered))

	// m and n skipped.
	skipped := append([]discovery.StackInfo(nil), all...)
	skipped[1].Skip, skipped[2].Skip = true, true
	check("skipped", mustGenerate(t, skipped, opts))
}

// A literal block line ending in whitespace would be stripped by trailing
// whitespace fixers, changing the command and failing --check.
func TestMarshalYAMLHasNoTrailingWhitespace(t *testing.T) {
	t.Parallel()

	cfg := mustGenerate(t, []discovery.StackInfo{{Dir: "a"}}, Options{CreateProjectName: true})
	cfg.Workflows = map[string]interface{}{"wf": map[string]interface{}{"plan": map[string]interface{}{"steps": []interface{}{
		map[string]interface{}{"run": "echo a\t\necho b"},
		map[string]interface{}{"run": "echo c \necho d\n"},
		map[string]interface{}{"run": "echo e\necho f\n"},
	}}}}

	out, err := MarshalYAML(cfg, true)
	if err != nil {
		t.Fatal(err)
	}
	for i, line := range strings.Split(string(out), "\n") {
		if strings.TrimRight(line, " \t") != line {
			t.Errorf("line %d has trailing whitespace: %q", i+1, line)
		}
	}

	var back Config
	if err := yaml.Unmarshal(out, &back); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(back.Workflows, cfg.Workflows) {
		t.Fatalf("round trip changed workflows:\n%#v\n%#v", back.Workflows, cfg.Workflows)
	}
	if !strings.Contains(string(out), "- run: |\n") {
		t.Errorf("multi-line strings without trailing whitespace keep the literal style:\n%s", out)
	}
}

// With depends_on, a cycle makes the projects impossible to apply in
// Atlantis; the warning must say so rather than only mention ordering.
func TestDependencyCycleWarningMentionsDependsOn(t *testing.T) {
	t.Parallel()

	var warnings []string
	mustGenerate(t, []discovery.StackInfo{
		{Dir: "a", AfterDirs: []string{"b"}},
		{Dir: "b", AfterDirs: []string{"a"}},
	}, Options{
		CreateProjectName: true,
		DependsOn:         true,
		Warnf:             func(format string, args ...any) { warnings = append(warnings, fmt.Sprintf(format, args...)) },
	})
	if len(warnings) != 1 || !strings.Contains(warnings[0], "stacks a, b;") || !strings.Contains(warnings[0], "can't be applied") {
		t.Fatalf("unexpected warnings: %q", warnings)
	}
}

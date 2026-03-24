package atlantis

import (
	"reflect"
	"strings"
	"testing"

	"github.com/devops-roast/terramate-atlantis-config/internal/discovery"
)

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

			got := Generate(tc.stacks, tc.opts)
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
}`

	if string(got) != want {
		t.Fatalf("unexpected json:\n%s\nwant:\n%s", string(got), want)
	}
}

func TestGenerateExecutionOrderGroupsAndDependsOn(t *testing.T) {
	t.Parallel()

	stacks := []discovery.StackInfo{
		{Dir: "env/a", After: []string{"../b"}},
		{Dir: "env/b"},
		{Dir: "env/c", Before: []string{"../b"}},
	}

	cfg := Generate(stacks, Options{
		Autoplan:             true,
		Parallel:             true,
		WhenModified:         []string{"*.tf"},
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

	if !reflect.DeepEqual(projects["env/a"].DependsOn, []string{"env/b"}) {
		t.Fatalf("env/a depends_on: got %#v", projects["env/a"].DependsOn)
	}

	if !reflect.DeepEqual(projects["env/b"].DependsOn, []string{"env/c"}) {
		t.Fatalf("env/b depends_on: got %#v", projects["env/b"].DependsOn)
	}
}

func TestPreserveWorkflows(t *testing.T) {
	t.Parallel()

	existing := []byte(`version: 3
workflows:
  custom:
    plan:
      steps:
        - init
projects:
  - dir: keep/me
`)

	cfg := Config{Version: 3}
	err := PreserveWorkflows(existing, &cfg)
	if err != nil {
		t.Fatalf("preserve workflows failed: %v", err)
	}

	if _, ok := cfg.Workflows["custom"]; !ok {
		t.Fatalf("expected preserved custom workflow, got %#v", cfg.Workflows)
	}
}

func TestGenerateApplyRequirementsPrecedence(t *testing.T) {
	t.Parallel()

	stacks := []discovery.StackInfo{
		{Dir: "stack/a", Tags: []string{"prod"}, ApplyRequirements: []string{"approved"}},
		{Dir: "stack/b", Tags: []string{"prod"}},
		{Dir: "stack/c"},
	}

	cfg := Generate(stacks, Options{
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

	if !reflect.DeepEqual(projects["stack/a"].ApplyRequirements, []string{"approved"}) {
		t.Fatalf("stack/a apply_requirements: got %#v", projects["stack/a"].ApplyRequirements)
	}

	if !reflect.DeepEqual(projects["stack/b"].ApplyRequirements, []string{"policy_check"}) {
		t.Fatalf("stack/b apply_requirements: got %#v", projects["stack/b"].ApplyRequirements)
	}

	if !reflect.DeepEqual(projects["stack/c"].ApplyRequirements, []string{"mergeable"}) {
		t.Fatalf("stack/c apply_requirements: got %#v", projects["stack/c"].ApplyRequirements)
	}
}

func TestGenerateCreateWorkspace(t *testing.T) {
	t.Parallel()

	cfg := Generate([]discovery.StackInfo{{Dir: "aws/prod"}}, Options{
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

	cfg := Generate(stacks, Options{
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

	cfg := Generate(stacks, Options{
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

	cfg := Generate([]discovery.StackInfo{{Dir: "stacks/aws/prod/vpc"}}, Options{
		Autoplan:          true,
		Parallel:          true,
		CreateProjectName: true,
		WhenModified:      []string{"*.tf"},
	})

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

	cfg := Generate(stacks, Options{
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

	cfg := Generate(stacks, Options{
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

	cfg := Generate(stacks, Options{
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

	cfg := Generate(stacks, Options{
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

	cfg := Generate(stacks, Options{
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

	cfg := Generate(stacks, Options{
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

	cfg := Generate(stacks, Options{
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

	cfg := Generate(stacks, Options{
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

	cfg := Generate(stacks, Options{
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

	cfg := Generate(stacks, Options{
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
		{Dir: "z/prod", After: []string{"/a/prod"}},
		{Dir: "a/prod", After: []string{"/m/prod"}},
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

			cfg := Generate(stacks, Options{
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

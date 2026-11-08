package cmd

import (
	"reflect"
	"testing"

	"github.com/devops-roast/terramate-atlantis-config/internal/atlantis"
)

func TestValidateConfig(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		cfg  atlantis.Config
		want []string
	}{
		{
			name: "valid config with dotted names and depends_on by name",
			cfg: atlantis.Config{
				Version: 3,
				Projects: []atlantis.Project{
					{Name: "dev-v1.2", Dir: "stacks/dev/v1.2"},
					{Name: "root", Dir: "."},
					{Name: "app", Dir: "stacks/app", DependsOn: []string{"dev-v1.2", "root"}},
				},
			},
			want: []string{},
		},
		{
			name: "same dir with different workspaces is allowed",
			cfg: atlantis.Config{
				Version: 3,
				Projects: []atlantis.Project{
					{Dir: "stacks/app", Workspace: "staging"},
					{Dir: "stacks/app", Workspace: "production"},
					{Dir: "stacks/app"},
				},
			},
			want: []string{},
		},
		{
			name: "duplicate dir and workspace",
			cfg: atlantis.Config{
				Version: 3,
				Projects: []atlantis.Project{
					{Dir: "stacks/app"},
					{Dir: "stacks/app", Workspace: "default"},
				},
			},
			want: []string{"duplicate project dir: stacks/app (workspace default); projects sharing a dir and workspace must all have a name"},
		},
		{
			name: "same dir and workspace is allowed when every project is named",
			cfg: atlantis.Config{
				Version: 3,
				Projects: []atlantis.Project{
					{Name: "app-staging", Dir: "stacks/app", Workflow: "staging"},
					{Name: "app-prod", Dir: "stacks/app", Workflow: "prod"},
				},
			},
			want: []string{},
		},
		{
			name: "same dir and workspace with one unnamed project",
			cfg: atlantis.Config{
				Version: 3,
				Projects: []atlantis.Project{
					{Dir: "stacks/app"},
					{Name: "app-prod", Dir: "stacks/app"},
				},
			},
			want: []string{"duplicate project dir: stacks/app (workspace default); projects sharing a dir and workspace must all have a name"},
		},
		{
			name: "depends_on with dir instead of project name",
			cfg: atlantis.Config{
				Version: 3,
				Projects: []atlantis.Project{
					{Name: "network", Dir: "stacks/network"},
					{Name: "app", Dir: "stacks/app", DependsOn: []string{"stacks/network"}},
				},
			},
			want: []string{`project "app" depends_on unknown project name "stacks/network"`},
		},
		{
			name: "depends_on itself and from an unnamed project",
			cfg: atlantis.Config{
				Version: 3,
				Projects: []atlantis.Project{
					{Name: "app", Dir: "stacks/app", DependsOn: []string{"app"}},
					{Dir: "stacks/db", DependsOn: []string{"missing"}},
				},
			},
			want: []string{
				`project "app" depends on itself`,
				`project "stacks/db" depends_on unknown project name "missing"`,
			},
		},
		{
			name: "invalid names, missing dir, wrong version, unknown workflow",
			cfg: atlantis.Config{
				Version: 2,
				Projects: []atlantis.Project{
					{Name: "my stack", Dir: "a"},
					{Name: "ok", Dir: ""},
					{Name: "ok", Dir: "b", Workflow: "nope"},
				},
				Workflows: map[string]interface{}{"terramate": map[string]interface{}{}},
			},
			want: []string{
				"version must be 3",
				"invalid project name: my stack (must contain only URL safe characters)",
				"projects[1]: dir is required",
				"duplicate project name: ok",
				`project "b" references unknown workflow "nope"`,
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := validateConfig(tc.cfg)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("validateConfig:\n got  %#v\n want %#v", got, tc.want)
			}
		})
	}
}

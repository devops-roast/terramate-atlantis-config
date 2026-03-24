package globals

import (
	"reflect"
	"testing"

	"github.com/zclconf/go-cty/cty"
)

func TestExtractAtlantisGlobals(t *testing.T) {
	t.Parallel()

	falseVal := false

	testcases := []struct {
		name    string
		globals map[string]cty.Value
		want    AtlantisOverrides
		wantErr bool
	}{
		{
			name:    "empty map",
			globals: map[string]cty.Value{},
			want:    AtlantisOverrides{},
		},
		{
			name: "atlantis_skip true",
			globals: map[string]cty.Value{
				"atlantis_skip": cty.BoolVal(true),
			},
			want: AtlantisOverrides{Skip: true},
		},
		{
			name: "atlantis_workflow set",
			globals: map[string]cty.Value{
				"atlantis_workflow": cty.StringVal("custom"),
			},
			want: AtlantisOverrides{Workflow: "custom"},
		},
		{
			name: "atlantis_autoplan false",
			globals: map[string]cty.Value{
				"atlantis_autoplan": cty.BoolVal(false),
			},
			want: AtlantisOverrides{AutoplanEnabled: &falseVal},
		},
		{
			name: "atlantis_terraform_version set",
			globals: map[string]cty.Value{
				"atlantis_terraform_version": cty.StringVal("1.5.0"),
			},
			want: AtlantisOverrides{TerraformVersion: "1.5.0"},
		},
		{
			name: "atlantis_when_modified set",
			globals: map[string]cty.Value{
				"atlantis_when_modified": cty.ListVal([]cty.Value{cty.StringVal("*.tf"), cty.StringVal("*.tfvars")}),
			},
			want: AtlantisOverrides{WhenModified: []string{"*.tf", "*.tfvars"}},
		},
		{
			name: "atlantis_extra_deps set",
			globals: map[string]cty.Value{
				"atlantis_extra_deps": cty.ListVal([]cty.Value{cty.StringVal("../modules/**/*.tf")}),
			},
			want: AtlantisOverrides{ExtraDeps: []string{"../modules/**/*.tf"}},
		},
		{
			name: "atlantis_apply_requirements set",
			globals: map[string]cty.Value{
				"atlantis_apply_requirements": cty.ListVal([]cty.Value{cty.StringVal("approved"), cty.StringVal("mergeable")}),
			},
			want: AtlantisOverrides{ApplyRequirements: []string{"approved", "mergeable"}},
		},
		{
			name: "all fields set",
			globals: map[string]cty.Value{
				"atlantis_skip":               cty.BoolVal(true),
				"atlantis_workflow":           cty.StringVal("custom-workflow"),
				"atlantis_autoplan":           cty.BoolVal(false),
				"atlantis_terraform_version":  cty.StringVal("1.5.0"),
				"atlantis_extra_deps":         cty.ListVal([]cty.Value{cty.StringVal("../modules/**/*.tf")}),
				"atlantis_when_modified":      cty.ListVal([]cty.Value{cty.StringVal("*.tf"), cty.StringVal("*.tfvars")}),
				"atlantis_apply_requirements": cty.ListVal([]cty.Value{cty.StringVal("approved")}),
			},
			want: AtlantisOverrides{
				Skip:              true,
				Workflow:          "custom-workflow",
				AutoplanEnabled:   &falseVal,
				TerraformVersion:  "1.5.0",
				ExtraDeps:         []string{"../modules/**/*.tf"},
				WhenModified:      []string{"*.tf", "*.tfvars"},
				ApplyRequirements: []string{"approved"},
			},
		},
		{
			name: "non-atlantis keys ignored",
			globals: map[string]cty.Value{
				"environment": cty.StringVal("prod"),
				"foo":         cty.StringVal("bar"),
			},
			want: AtlantisOverrides{},
		},
		{
			name: "wrong type for atlantis_skip",
			globals: map[string]cty.Value{
				"atlantis_skip": cty.StringVal("true"),
			},
			wantErr: true,
		},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := ExtractAtlantisGlobals(tc.globals)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("unexpected overrides: got %#v, want %#v", got, tc.want)
			}
		})
	}
}

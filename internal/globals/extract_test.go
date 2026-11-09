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
			name: "null values are treated as unset",
			globals: map[string]cty.Value{
				"atlantis_skip":          cty.NullVal(cty.Bool),
				"atlantis_workflow":      cty.NullVal(cty.String),
				"atlantis_extra_deps":    cty.NullVal(cty.List(cty.String)),
				"atlantis_when_modified": cty.NullVal(cty.DynamicPseudoType),
			},
			want: AtlantisOverrides{},
		},
		{
			name: "null list element is an error, not a panic",
			globals: map[string]cty.Value{
				"atlantis_extra_deps": cty.ListVal([]cty.Value{cty.StringVal("a"), cty.NullVal(cty.String)}),
			},
			wantErr: true,
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

func TestExtractAtlantisGlobalsReportsUnknownKeys(t *testing.T) {
	t.Parallel()

	got, err := ExtractAtlantisGlobals(map[string]cty.Value{
		"atlantis_workflows": cty.StringVal("typo"),
		"atlantis_skp":       cty.True,
		"atlantis_workflow":  cty.StringVal("prod"),
		"other_global":       cty.StringVal("ignored"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"atlantis_skp", "atlantis_workflows"}; !reflect.DeepEqual(got.UnknownKeys, want) {
		t.Fatalf("UnknownKeys: got %v, want %v", got.UnknownKeys, want)
	}
	if got.Workflow != "prod" {
		t.Fatalf("known keys must still be read, got workflow %q", got.Workflow)
	}
}

// With several invalid globals the error must not depend on map iteration
// order: the same stack always reports the same (first, by name) global.
func TestExtractAtlantisGlobalsReportsErrorsDeterministically(t *testing.T) {
	t.Parallel()

	globals := map[string]cty.Value{
		"atlantis_workflow":           cty.NumberIntVal(1),
		"atlantis_skip":               cty.StringVal("yes"),
		"atlantis_apply_requirements": cty.True,
		"atlantis_terraform_version":  cty.True,
	}
	for i := 0; i < 50; i++ {
		_, err := ExtractAtlantisGlobals(globals)
		if err == nil || err.Error() != "atlantis_apply_requirements must be list of strings" {
			t.Fatalf("run %d: got %v, want the error for atlantis_apply_requirements", i, err)
		}
	}
}

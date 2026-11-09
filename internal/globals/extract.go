package globals

import (
	"fmt"
	"sort"
	"strings"

	"github.com/zclconf/go-cty/cty"
)

// AtlantisOverrides contains per-stack Atlantis settings from globals.
type AtlantisOverrides struct {
	Skip              bool
	Workflow          string
	AutoplanEnabled   *bool
	TerraformVersion  string
	ExtraDeps         []string
	WhenModified      []string
	ApplyRequirements []string
	// UnknownKeys lists atlantis_* globals that are not one of KnownGlobals
	// (usually a typo such as atlantis_workflows), sorted.
	UnknownKeys []string
}

// KnownGlobals are the atlantis_* globals read by ExtractAtlantisGlobals.
var KnownGlobals = []string{
	"atlantis_apply_requirements",
	"atlantis_autoplan",
	"atlantis_extra_deps",
	"atlantis_skip",
	"atlantis_terraform_version",
	"atlantis_when_modified",
	"atlantis_workflow",
}

// ExtractAtlantisGlobals extracts atlantis_* keys from a globals map.
func ExtractAtlantisGlobals(globals map[string]cty.Value) (AtlantisOverrides, error) {
	var overrides AtlantisOverrides

	// Sorted, so that with several invalid globals the same one is always
	// reported.
	keys := make([]string, 0, len(globals))
	for key := range globals {
		if strings.HasPrefix(key, "atlantis_") {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)

	for _, key := range keys {
		val := globals[key]
		// A null value (e.g. from `cond ? [...] : null`) means "not set";
		// cty panics on most accessors for null values.
		if val.IsNull() {
			continue
		}

		switch key {
		case "atlantis_skip":
			if !val.Type().Equals(cty.Bool) {
				return AtlantisOverrides{}, fmt.Errorf("%s must be bool", key)
			}
			overrides.Skip = val.True()
		case "atlantis_workflow":
			if !val.Type().Equals(cty.String) {
				return AtlantisOverrides{}, fmt.Errorf("%s must be string", key)
			}
			overrides.Workflow = val.AsString()
		case "atlantis_autoplan":
			if !val.Type().Equals(cty.Bool) {
				return AtlantisOverrides{}, fmt.Errorf("%s must be bool", key)
			}
			autoplan := val.True()
			overrides.AutoplanEnabled = &autoplan
		case "atlantis_terraform_version":
			if !val.Type().Equals(cty.String) {
				return AtlantisOverrides{}, fmt.Errorf("%s must be string", key)
			}
			overrides.TerraformVersion = val.AsString()
		case "atlantis_extra_deps":
			extraDeps, err := asStringSlice(key, val)
			if err != nil {
				return AtlantisOverrides{}, err
			}
			overrides.ExtraDeps = extraDeps
		case "atlantis_when_modified":
			whenModified, err := asStringSlice(key, val)
			if err != nil {
				return AtlantisOverrides{}, err
			}
			overrides.WhenModified = whenModified
		case "atlantis_apply_requirements":
			applyRequirements, err := asStringSlice(key, val)
			if err != nil {
				return AtlantisOverrides{}, err
			}
			overrides.ApplyRequirements = applyRequirements
		default:
			overrides.UnknownKeys = append(overrides.UnknownKeys, key)
		}
	}

	return overrides, nil
}

func asStringSlice(key string, val cty.Value) ([]string, error) {
	if !val.Type().IsListType() && !val.Type().IsTupleType() {
		return nil, fmt.Errorf("%s must be list of strings", key)
	}

	values := val.AsValueSlice()
	result := make([]string, 0, len(values))
	for _, item := range values {
		if !item.Type().Equals(cty.String) || item.IsNull() {
			return nil, fmt.Errorf("%s must be list of strings", key)
		}
		result = append(result, item.AsString())
	}

	return result, nil
}

package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestLoadConfig(t *testing.T) {
	t.Parallel()

	t.Run("missing file returns nil", func(t *testing.T) {
		t.Parallel()

		root := t.TempDir()
		cfg, err := LoadConfig(root)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg != nil {
			t.Fatalf("expected nil config, got %#v", cfg)
		}
	})

	t.Run("loads yaml file", func(t *testing.T) {
		t.Parallel()

		root := t.TempDir()
		content := "output: atlantis.yaml\nautoplan: false\nparallel: false\nworkflow: terramate\n"
		path := filepath.Join(root, ".terramate-atlantis.yaml")
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("writing config: %v", err)
		}

		cfg, err := LoadConfig(root)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg == nil {
			t.Fatal("expected config, got nil")
		}

		if cfg.Output != "atlantis.yaml" {
			t.Fatalf("output: got %q", cfg.Output)
		}
		if cfg.Autoplan == nil || *cfg.Autoplan {
			t.Fatalf("autoplan: got %#v", cfg.Autoplan)
		}
		if cfg.Parallel == nil || *cfg.Parallel {
			t.Fatalf("parallel: got %#v", cfg.Parallel)
		}
		if cfg.Workflow != "terramate" {
			t.Fatalf("workflow: got %q", cfg.Workflow)
		}
	})

	t.Run("loads yml file", func(t *testing.T) {
		t.Parallel()

		root := t.TempDir()
		if err := os.WriteFile(filepath.Join(root, ".terramate-atlantis.yml"), []byte("workflow: x\n"), 0o644); err != nil {
			t.Fatalf("writing config: %v", err)
		}

		cfg, err := LoadConfig(root)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg == nil || cfg.Workflow != "x" {
			t.Fatalf("workflow: got %#v", cfg)
		}
	})

	t.Run("invalid file is an error, not ignored", func(t *testing.T) {
		t.Parallel()

		root := t.TempDir()
		if err := os.WriteFile(filepath.Join(root, ".terramate-atlantis.yaml"), []byte("bogus: 1\n"), 0o644); err != nil {
			t.Fatalf("writing config: %v", err)
		}

		if _, err := LoadConfig(root); err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestLoadConfigFromPath(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	path := filepath.Join(root, "custom.yaml")
	if err := os.WriteFile(path, []byte("format: json\nsort_by: name\n"), 0o644); err != nil {
		t.Fatalf("writing config: %v", err)
	}

	cfg, err := LoadConfigFromPath(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Format != "json" {
		t.Fatalf("format: got %q", cfg.Format)
	}
	if cfg.SortBy != "name" {
		t.Fatalf("sort_by: got %q", cfg.SortBy)
	}
}

func writeConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), ".terramate-atlantis.yaml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("writing config: %v", err)
	}
	return path
}

func TestTagRequirementsForms(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"flow list":    "tag_requirements:\n  prod: [approved, mergeable]\n",
		"block list":   "tag_requirements:\n  prod:\n    - approved\n    - mergeable\n",
		"comma string": "tag_requirements:\n  prod: approved, mergeable\n",
	}

	for name, content := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			cfg, err := LoadConfigFromPath(writeConfig(t, content))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			got := []string(cfg.TagRequirements["prod"])
			if !reflect.DeepEqual(got, []string{"approved", "mergeable"}) {
				t.Fatalf("tag_requirements.prod: got %#v", got)
			}
		})
	}

	t.Run("mapping value is rejected", func(t *testing.T) {
		t.Parallel()

		_, err := LoadConfigFromPath(writeConfig(t, "tag_requirements:\n  prod:\n    a: b\n"))
		if err == nil {
			t.Fatal("expected error for mapping value")
		}
	})
}

func TestFlagEquivalentKeysLoad(t *testing.T) {
	t.Parallel()

	content := `output: atlantis.yaml
format: json
no_header: true
log_level: error
changed: true
changed_base_ref: origin/main
preserve_workflows: true
generate_workflows: true
workflow_terramate_wrap: false
workflow_disable_safeguards: [git-untracked, outdated-code]
`
	cfg, err := LoadConfigFromPath(writeConfig(t, content))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	checks := []struct {
		name string
		ok   bool
	}{
		{"no_header", cfg.NoHeader != nil && *cfg.NoHeader},
		{"log_level", cfg.LogLevel == "error"},
		{"changed", cfg.Changed != nil && *cfg.Changed},
		{"changed_base_ref", cfg.ChangedBaseRef == "origin/main"},
		{"preserve_workflows", cfg.PreserveWorkflows != nil && *cfg.PreserveWorkflows},
		{"generate_workflows", cfg.GenerateWorkflows != nil && *cfg.GenerateWorkflows},
		{"workflow_terramate_wrap", cfg.WorkflowTerramateWrap != nil && !*cfg.WorkflowTerramateWrap},
		{"workflow_disable_safeguards", cfg.WorkflowDisableSafeguards != nil &&
			reflect.DeepEqual([]string(*cfg.WorkflowDisableSafeguards), []string{"git-untracked", "outdated-code"})},
	}
	for _, c := range checks {
		if !c.ok {
			t.Errorf("%s not loaded: %#v", c.name, cfg)
		}
	}
}

func TestEmptySafeguardListIsDistinctFromUnset(t *testing.T) {
	t.Parallel()

	cfg, err := LoadConfigFromPath(writeConfig(t, "workflow_disable_safeguards: []\n"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.WorkflowDisableSafeguards == nil || len(*cfg.WorkflowDisableSafeguards) != 0 {
		t.Fatalf("expected explicit empty list, got %#v", cfg.WorkflowDisableSafeguards)
	}

	cfg, err = LoadConfigFromPath(writeConfig(t, "output: x\n"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.WorkflowDisableSafeguards != nil {
		t.Fatalf("expected unset, got %#v", cfg.WorkflowDisableSafeguards)
	}
}

func TestUnknownKeysRejected(t *testing.T) {
	t.Parallel()

	t.Run("top-level typo names the key and line", func(t *testing.T) {
		t.Parallel()

		_, err := LoadConfigFromPath(writeConfig(t, "output: atlantis.yaml\nchnaged: true\n"))
		if err == nil {
			t.Fatal("expected error for unknown key")
		}
		msg := err.Error()
		for _, want := range []string{`unknown key "chnaged"`, "line 2", "changed_base_ref"} {
			if !strings.Contains(msg, want) {
				t.Errorf("error %q does not contain %q", msg, want)
			}
		}
	})

	t.Run("not a mapping", func(t *testing.T) {
		t.Parallel()

		if _, err := LoadConfigFromPath(writeConfig(t, "- a\n- b\n")); err == nil {
			t.Fatal("expected error for non-mapping config")
		}
	})

	t.Run("wrong type", func(t *testing.T) {
		t.Parallel()

		if _, err := LoadConfigFromPath(writeConfig(t, "changed: maybe\n")); err == nil {
			t.Fatal("expected error for wrong type")
		}
	})
}

func TestEmptyConfigFile(t *testing.T) {
	t.Parallel()

	for name, content := range map[string]string{
		"empty":                  "",
		"comment only":           "# nothing here\n",
		"document start only":    "---\n# output: atlantis.yaml\n",
		"explicit null document": "--- ~\n",
		"document start and end": "---\n...\n",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			cfg, err := LoadConfigFromPath(writeConfig(t, content))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if cfg == nil {
				t.Fatal("expected empty config, got nil")
			}
		})
	}
}

func TestKnownKeysCoverEveryField(t *testing.T) {
	t.Parallel()

	if got, want := len(KnownKeys()), reflect.TypeOf(FileConfig{}).NumField(); got != want {
		t.Fatalf("expected one key per field: got %d keys for %d fields", got, want)
	}
}

// Only the first YAML document used to be read, so keys in any later
// document were silently ignored, defeating the unknown-key check.
func TestMultipleDocumentsRejected(t *testing.T) {
	t.Parallel()

	for _, content := range []string{
		"---\n---\nformat: json\n",
		"format: json\n---\nformat: yaml\n",
		"format: json\n---\nformta: yaml\n",
	} {
		if _, err := parse([]byte(content)); err == nil || !strings.Contains(err.Error(), "single YAML document") {
			t.Errorf("parse(%q): got error %v, want a single-document error", content, err)
		}
	}

	// An empty trailing document ("---" at the end) adds nothing.
	for _, content := range []string{"format: json\n---\n", "---\nformat: json\n", "format: json\n---\n# comment\n"} {
		cfg, err := parse([]byte(content))
		if err != nil || cfg.Format != "json" {
			t.Errorf("parse(%q) = %+v, %v; want format json", content, cfg, err)
		}
	}
}

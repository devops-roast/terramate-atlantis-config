package config

import (
	"os"
	"path/filepath"
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

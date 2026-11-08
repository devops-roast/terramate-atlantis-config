package config

import (
	"strings"
	"testing"
)

// FuzzParse checks that any input either fails cleanly or yields string
// lists whose entries are trimmed and non-empty, whatever form (sequence,
// comma-separated scalar, null, alias, mixed scalar types) they used.
func FuzzParse(f *testing.F) {
	for _, seed := range []string{
		"",
		"---\n",
		"workflow_disable_safeguards: git, outdated-code\n",
		"workflow_disable_safeguards: [git, ' ', 1, true, null]\n",
		"workflow_disable_safeguards: ~\n",
		"workflow_disable_safeguards: []\n",
		"workflow_disable_safeguards: {a: b}\n",
		"workflow_disable_safeguards: [[git]]\n",
		"tag_requirements:\n  prod: &r [approved, mergeable]\n  dev: *r\n",
		"tag_requirements:\n  prod: approved,,mergeable\n",
		"tag_requirements: [a]\n",
		"when_modified: []\napply_requirements: [approved]\n",
		"workflows:\n  x:\n    plan:\n      steps: [init]\n",
		"unknown: 1\n",
		"- a\n",
	} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, content string) {
		cfg, err := parse([]byte(content))
		if err != nil {
			return
		}
		if cfg == nil {
			t.Fatal("nil config without error")
		}
		check := func(what string, list StringList) {
			for _, item := range list {
				if item == "" || item != strings.TrimSpace(item) {
					t.Fatalf("%s: untrimmed or empty entry %q in %q", what, item, list)
				}
			}
		}
		if cfg.WorkflowDisableSafeguards != nil {
			check("workflow_disable_safeguards", *cfg.WorkflowDisableSafeguards)
		}
		for tag, reqs := range cfg.TagRequirements {
			check("tag_requirements."+tag, reqs)
		}
	})
}

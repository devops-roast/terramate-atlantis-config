package cmd

import (
	"fmt"
	"os"
	"path"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/devops-roast/terramate-atlantis-config/internal/atlantis"
)

func newValidateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "validate [file]",
		Short: "Validate an atlantis.yaml file",
		Args:  cobra.ExactArgs(1),
		RunE:  runValidate,
	}
}

func runValidate(cmd *cobra.Command, args []string) error {
	content, err := os.ReadFile(args[0])
	if err != nil {
		return fmt.Errorf("reading %s: %w", args[0], err)
	}

	var cfg atlantis.Config
	if err := yaml.Unmarshal(content, &cfg); err != nil {
		return fmt.Errorf("invalid YAML syntax: %w", err)
	}

	for _, warning := range workflowWarnings(cfg) {
		fmt.Fprintf(cmd.ErrOrStderr(), "Warning: %s\n", warning)
	}

	errors := validateConfig(cfg)
	if len(errors) > 0 {
		for _, validationErr := range errors {
			fmt.Fprintln(cmd.ErrOrStderr(), validationErr)
		}
		return &ExitError{Code: 1}
	}

	fmt.Fprintf(cmd.OutOrStdout(), "%s is valid\n", args[0])
	return nil
}

func validateConfig(cfg atlantis.Config) []string {
	issues := []string{}

	if cfg.Version != 3 {
		issues = append(issues, "version must be 3")
	}

	// Atlantis identifies an unnamed project by dir + workspace. The same
	// dir + workspace may appear several times only if every such project
	// has a name, so each one can be targeted on its own.
	allNamedByDirWorkspace := map[string]bool{}
	seenNames := map[string]struct{}{}
	for i, project := range cfg.Projects {
		if project.Dir == "" {
			issues = append(issues, fmt.Sprintf("projects[%d]: dir is required", i))
		} else {
			workspace := project.Workspace
			if workspace == "" {
				workspace = "default"
			}
			// Atlantis cleans the dir ("./a", "a/" and "/a" are all "a")
			// before matching projects, so compare cleaned dirs.
			key := path.Clean("./"+project.Dir) + "\x00" + workspace
			named := project.Name != ""
			if allNamed, exists := allNamedByDirWorkspace[key]; exists {
				if !allNamed || !named {
					issues = append(issues, fmt.Sprintf(
						"duplicate project dir: %s (workspace %s); projects sharing a dir and workspace must all have a name",
						project.Dir, workspace))
				}
				named = named && allNamed
			}
			allNamedByDirWorkspace[key] = named
		}

		if project.Name != "" {
			if _, exists := seenNames[project.Name]; exists {
				issues = append(issues, fmt.Sprintf("duplicate project name: %s", project.Name))
			}
			seenNames[project.Name] = struct{}{}

			if !atlantis.ValidProjectName(project.Name) {
				issues = append(issues, fmt.Sprintf("invalid project name: %s (must contain only URL safe characters)", project.Name))
			}
		}
	}

	// Atlantis matches depends_on entries against project names.
	for _, project := range cfg.Projects {
		for _, dep := range project.DependsOn {
			if project.Name != "" && dep == project.Name {
				issues = append(issues, fmt.Sprintf("project %q depends on itself", project.Name))
				continue
			}
			if _, exists := seenNames[dep]; !exists {
				issues = append(issues, fmt.Sprintf("project %q depends_on unknown project name %q", projectRef(project), dep))
			}
		}
	}

	return issues
}

// workflowWarnings reports projects whose workflow is not defined in the
// file. That is not an error: Atlantis also looks the name up in the
// server-side repo config, so a file may mix its own workflows with
// server-side ones (e.g. --generate-workflows plus an atlantis_workflow
// global naming a server workflow). Only a file that defines workflows is
// checked; otherwise every workflow is server-side.
func workflowWarnings(cfg atlantis.Config) []string {
	if len(cfg.Workflows) == 0 {
		return nil
	}
	var warnings []string
	for _, project := range cfg.Projects {
		if project.Workflow == "" {
			continue
		}
		if _, exists := cfg.Workflows[project.Workflow]; !exists {
			warnings = append(warnings, fmt.Sprintf(
				"project %q uses workflow %q, which this file does not define; it must be defined in the server-side repo config",
				projectRef(project), project.Workflow))
		}
	}
	return warnings
}

func projectRef(project atlantis.Project) string {
	if project.Name != "" {
		return project.Name
	}
	return project.Dir
}

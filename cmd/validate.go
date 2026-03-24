package cmd

import (
	"fmt"
	"os"
	"regexp"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/devops-roast/terramate-atlantis-config/internal/atlantis"
)

var projectNameRe = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

var validateCmd = &cobra.Command{
	Use:   "validate [file]",
	Short: "Validate an atlantis.yaml file",
	Args:  cobra.ExactArgs(1),
	RunE:  runValidate,
}

func init() {
	rootCmd.AddCommand(validateCmd)
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

	errors := validateConfig(cfg)
	if len(errors) > 0 {
		for _, validationErr := range errors {
			fmt.Fprintln(os.Stderr, validationErr)
		}
		return &ExitError{Code: 1}
	}

	fmt.Fprintln(os.Stdout, "atlantis.yaml is valid")
	return nil
}

func validateConfig(cfg atlantis.Config) []string {
	issues := []string{}

	if cfg.Version != 3 {
		issues = append(issues, "version must be 3")
	}

	seenDirs := map[string]struct{}{}
	seenNames := map[string]struct{}{}
	for i, project := range cfg.Projects {
		if project.Dir == "" {
			issues = append(issues, fmt.Sprintf("projects[%d]: dir is required", i))
		} else {
			if _, exists := seenDirs[project.Dir]; exists {
				issues = append(issues, fmt.Sprintf("duplicate project dir: %s", project.Dir))
			}
			seenDirs[project.Dir] = struct{}{}
		}

		if project.Name != "" {
			if _, exists := seenNames[project.Name]; exists {
				issues = append(issues, fmt.Sprintf("duplicate project name: %s", project.Name))
			}
			seenNames[project.Name] = struct{}{}

			if !projectNameRe.MatchString(project.Name) {
				issues = append(issues, fmt.Sprintf("invalid project name: %s", project.Name))
			}
		}

		if project.Workflow != "" && len(cfg.Workflows) > 0 {
			if _, exists := cfg.Workflows[project.Workflow]; !exists {
				issues = append(issues, fmt.Sprintf("project %q references unknown workflow %q", project.Dir, project.Workflow))
			}
		}
	}

	return issues
}

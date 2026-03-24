package cmd

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/devops-roast/terramate-atlantis-config/internal/atlantis"
	internalconfig "github.com/devops-roast/terramate-atlantis-config/internal/config"
	"github.com/devops-roast/terramate-atlantis-config/internal/discovery"
)

// GenerateOptions holds all CLI flag values for the generate command.
type GenerateOptions struct {
	Root                  string
	Output                string
	Config                string
	Autoplan              bool
	Parallel              bool
	Automerge             bool
	Workflow              string
	TerraformVersion      string
	Filter                string
	FilterPath            string
	ExcludeTags           string
	ExcludePath           string
	Changed               bool
	ChangedBaseRef        string
	CreateProjectName     bool
	CreateWorkspace       bool
	PreserveWorkflows     bool
	GenerateWorkflows     bool
	WorkflowTerramateWrap bool
	Diff                  bool
	Check                 bool
	SortBy                string
	NoHeader              bool
	Format                string
	ExecutionOrderGroups  bool
	DependsOn             bool
	ProjectNameStrategy   string
	ProjectNamePrefix     string
	ProjectNameDepth      int
	WhenModified          []string
	ApplyRequirements     []string
	TagWorkflow           []string
	TagRequirements       []string
	ConfigWorkflows       map[string]interface{}
}

var opts GenerateOptions

var generateCmd = &cobra.Command{
	Use:   "generate",
	Short: "Generate atlantis.yaml from Terramate stacks",
	Long: `Discovers all Terramate stacks in the repository and generates
an atlantis.yaml configuration file. Per-stack customization is
supported via Terramate globals (atlantis_skip, atlantis_workflow, etc).`,
	RunE: runGenerate,
}

func init() {
	rootCmd.AddCommand(generateCmd)

	generateCmd.Flags().StringVar(&opts.Root, "root", ".", "Path to the root directory of the Terramate project")
	generateCmd.Flags().StringVar(&opts.Output, "output", "", "Output file path (default: stdout)")
	generateCmd.Flags().StringVar(&opts.Config, "config", "", "Path to configuration file (default: .terramate-atlantis.yaml in root)")
	generateCmd.Flags().BoolVar(&opts.Autoplan, "autoplan", true, "Enable autoplan for all projects")
	generateCmd.Flags().BoolVar(&opts.Parallel, "parallel", true, "Enable parallel plan and apply")
	generateCmd.Flags().BoolVar(&opts.Automerge, "automerge", false, "Enable automerge")
	generateCmd.Flags().StringVar(&opts.Workflow, "workflow", "", "Default workflow name for all projects")
	generateCmd.Flags().StringVar(&opts.TerraformVersion, "terraform-version", "", "Default terraform version for all projects")
	generateCmd.Flags().StringVar(&opts.Filter, "filter", "", "Filter stacks by tags (Terramate tag filter syntax)")
	generateCmd.Flags().StringVar(&opts.FilterPath, "filter-path", "", "Filter stacks by directory path glob")
	generateCmd.Flags().StringVar(&opts.ExcludeTags, "exclude-tags", "", "Exclude stacks by tags (Terramate tag filter syntax)")
	generateCmd.Flags().StringVar(&opts.ExcludePath, "exclude-path", "", "Exclude stacks by directory path glob")
	generateCmd.Flags().BoolVar(&opts.Changed, "changed", false, "Only include stacks changed from the base ref")
	generateCmd.Flags().StringVar(&opts.ChangedBaseRef, "changed-base-ref", "main", "Base git ref for --changed stack detection")
	generateCmd.Flags().BoolVar(&opts.CreateProjectName, "create-project-name", true, "Generate project names from stack paths")
	generateCmd.Flags().BoolVar(&opts.CreateWorkspace, "create-workspace", false, "Generate workspace values from stack paths")
	generateCmd.Flags().BoolVar(&opts.PreserveWorkflows, "preserve-workflows", false, "Preserve workflows from an existing output file")
	generateCmd.Flags().BoolVar(&opts.GenerateWorkflows, "generate-workflows", false, "Generate default Atlantis workflows that run via terramate")
	generateCmd.Flags().BoolVar(&opts.WorkflowTerramateWrap, "workflow-terramate-wrap", true, "Wrap generated workflow terraform commands with terramate run --")
	generateCmd.Flags().BoolVar(&opts.Diff, "diff", false, "Print diff between existing output file and generated content")
	generateCmd.Flags().BoolVar(&opts.Check, "check", false, "Fail if output file differs from generated content")
	generateCmd.Flags().StringVar(&opts.SortBy, "sort-by", "dir", "Project sort order: dir, name, execution_order_group")
	generateCmd.Flags().BoolVar(&opts.NoHeader, "no-header", false, "Suppress generated file header comment")
	generateCmd.Flags().StringVar(&opts.Format, "format", "yaml", "Output format: yaml or json")
	generateCmd.Flags().BoolVar(&opts.ExecutionOrderGroups, "execution-order-groups", false, "Populate execution_order_group based on stack dependencies")
	generateCmd.Flags().BoolVar(&opts.DependsOn, "depends-on", false, "Populate depends_on from stack after/before dependencies")
	generateCmd.Flags().StringVar(&opts.ProjectNameStrategy, "project-name-strategy", "auto-strip", "Naming strategy: auto-strip, stack-name, last-n, full")
	generateCmd.Flags().StringVar(&opts.ProjectNamePrefix, "project-name-prefix", "", "Strip this prefix from stack paths when generating names")
	generateCmd.Flags().IntVar(&opts.ProjectNameDepth, "project-name-depth", 3, "Number of trailing path segments for last-n strategy")
	generateCmd.Flags().StringSliceVar(&opts.WhenModified, "when-modified", []string{"*.tf", "*.tf.json", ".terraform.lock.hcl"}, "Default when_modified patterns")
	generateCmd.Flags().StringSliceVar(&opts.ApplyRequirements, "apply-requirements", nil, "Default apply_requirements entries")
	generateCmd.Flags().StringSliceVar(&opts.TagWorkflow, "tag-workflow", nil, "Tag to workflow mapping (format: tag=workflow)")
	generateCmd.Flags().StringSliceVar(&opts.TagRequirements, "tag-requirements", nil, "Tag to requirements mapping (format: tag=req1,req2)")
}

func runGenerate(cmd *cobra.Command, args []string) error {
	if opts.Check && opts.Diff {
		return fmt.Errorf("--check and --diff are mutually exclusive")
	}

	if (opts.Check || opts.Diff) && (opts.Output == "" || opts.Output == "-") {
		return fmt.Errorf("--check and --diff require --output")
	}

	if opts.Format != "yaml" && opts.Format != "json" {
		return fmt.Errorf("invalid --format %q, expected yaml or json", opts.Format)
	}

	fileCfg, err := loadFileConfig(opts.Root, opts.Config)
	if err != nil {
		return fmt.Errorf("loading config file: %w", err)
	}

	if err := mergeConfig(cmd, &opts, fileCfg); err != nil {
		return err
	}

	tagWorkflows, err := parseTagWorkflows(opts.TagWorkflow)
	if err != nil {
		return err
	}

	tagRequirements, err := parseTagRequirements(opts.TagRequirements)
	if err != nil {
		return err
	}

	stacks, err := discovery.Discover(discovery.DiscoverOptions{
		RootDir:     opts.Root,
		TagFilter:   opts.Filter,
		PathFilter:  opts.FilterPath,
		ExcludeTags: opts.ExcludeTags,
		ExcludePath: opts.ExcludePath,
		Changed:     opts.Changed,
		BaseRef:     opts.ChangedBaseRef,
	})
	if err != nil {
		return fmt.Errorf("discovering stacks: %w", err)
	}

	cfg := atlantis.Generate(stacks, atlantis.Options{
		Autoplan:             opts.Autoplan,
		Parallel:             opts.Parallel,
		Automerge:            opts.Automerge,
		DefaultWorkflow:      opts.Workflow,
		TerraformVersion:     opts.TerraformVersion,
		CreateProjectName:    opts.CreateProjectName,
		CreateWorkspace:      opts.CreateWorkspace,
		WhenModified:         opts.WhenModified,
		ExecutionOrderGroups: opts.ExecutionOrderGroups,
		DependsOn:            opts.DependsOn,
		ApplyRequirements:    opts.ApplyRequirements,
		TagWorkflows:         tagWorkflows,
		TagRequirements:      tagRequirements,
		SortBy:               opts.SortBy,
		ProjectNameStrategy:  opts.ProjectNameStrategy,
		ProjectNamePrefix:    opts.ProjectNamePrefix,
		ProjectNameDepth:     opts.ProjectNameDepth,
	})

	if len(opts.ConfigWorkflows) > 0 {
		cfg.Workflows = make(map[string]interface{}, len(opts.ConfigWorkflows))
		for name, workflow := range opts.ConfigWorkflows {
			cfg.Workflows[name] = workflow
		}
	}

	generatedWorkflows := atlantis.GenerateWorkflows(atlantis.WorkflowOptions{
		Enabled:           opts.GenerateWorkflows,
		WrapWithTerramate: opts.WorkflowTerramateWrap,
	})
	if len(generatedWorkflows) > 0 {
		if cfg.Workflows == nil {
			cfg.Workflows = make(map[string]interface{}, len(generatedWorkflows))
		}
		for name, workflow := range generatedWorkflows {
			cfg.Workflows[name] = workflow
		}
	}

	if opts.PreserveWorkflows && opts.Output != "" && opts.Output != "-" {
		existing, readErr := os.ReadFile(opts.Output)
		if readErr == nil {
			if err := atlantis.PreserveWorkflows(existing, &cfg); err != nil {
				return fmt.Errorf("preserving workflows: %w", err)
			}
		} else if !os.IsNotExist(readErr) {
			return fmt.Errorf("reading existing output file: %w", readErr)
		}
	}

	var output []byte
	switch opts.Format {
	case "json":
		output, err = atlantis.MarshalJSON(cfg)
	default:
		output, err = atlantis.MarshalYAML(cfg, !opts.NoHeader)
	}
	if err != nil {
		return fmt.Errorf("marshaling config: %w", err)
	}

	if opts.Diff {
		existing, readErr := os.ReadFile(opts.Output)
		if readErr != nil && !os.IsNotExist(readErr) {
			return fmt.Errorf("reading existing output file: %w", readErr)
		}

		diff, hasChanges := atlantis.Diff(existing, output, filepath.Base(opts.Output))
		if hasChanges {
			fmt.Print(diff)
			return &ExitError{Code: 1}
		}
		return nil
	}

	if opts.Check {
		existing, readErr := os.ReadFile(opts.Output)
		if readErr != nil {
			if os.IsNotExist(readErr) {
				fmt.Fprintln(os.Stderr, "atlantis.yaml is out of date, run 'terramate-atlantis-config generate' to update")
				return &ExitError{Code: 1}
			}
			return fmt.Errorf("reading existing output file: %w", readErr)
		}

		if bytes.Equal(existing, output) {
			fmt.Fprintln(os.Stderr, "atlantis.yaml is up to date")
			return nil
		}

		fmt.Fprintln(os.Stderr, "atlantis.yaml is out of date, run 'terramate-atlantis-config generate' to update")
		return &ExitError{Code: 1}
	}

	if opts.Output == "" || opts.Output == "-" {
		fmt.Print(string(output))
		return nil
	}

	if err := os.WriteFile(opts.Output, output, 0o644); err != nil {
		return fmt.Errorf("writing output file: %w", err)
	}

	fmt.Fprintf(os.Stderr, "Generated %s with %d projects\n", opts.Output, len(cfg.Projects))
	return nil
}

func parseTagWorkflows(entries []string) (map[string]string, error) {
	mapping := make(map[string]string, len(entries))

	for _, entry := range entries {
		tag, workflow, ok := strings.Cut(entry, "=")
		if !ok || strings.TrimSpace(tag) == "" || strings.TrimSpace(workflow) == "" {
			return nil, fmt.Errorf("invalid --tag-workflow entry %q, expected tag=workflow", entry)
		}
		mapping[strings.TrimSpace(tag)] = strings.TrimSpace(workflow)
	}

	return mapping, nil
}

func loadFileConfig(rootDir, configPath string) (*internalconfig.FileConfig, error) {
	if configPath == "" {
		return internalconfig.LoadConfig(rootDir)
	}
	return internalconfig.LoadConfigFromPath(configPath)
}

func mergeConfig(cmd *cobra.Command, opts *GenerateOptions, cfg *internalconfig.FileConfig) error {
	if cfg == nil {
		return nil
	}

	if !cmd.Flags().Changed("output") && cfg.Output != "" {
		opts.Output = cfg.Output
	}
	if !cmd.Flags().Changed("autoplan") && cfg.Autoplan != nil {
		opts.Autoplan = *cfg.Autoplan
	}
	if !cmd.Flags().Changed("parallel") && cfg.Parallel != nil {
		opts.Parallel = *cfg.Parallel
	}
	if !cmd.Flags().Changed("automerge") && cfg.Automerge != nil {
		opts.Automerge = *cfg.Automerge
	}
	if !cmd.Flags().Changed("workflow") && cfg.Workflow != "" {
		opts.Workflow = cfg.Workflow
	}
	if !cmd.Flags().Changed("terraform-version") && cfg.TerraformVersion != "" {
		opts.TerraformVersion = cfg.TerraformVersion
	}
	if !cmd.Flags().Changed("filter") && cfg.Filter != "" {
		opts.Filter = cfg.Filter
	}
	if !cmd.Flags().Changed("filter-path") && cfg.FilterPath != "" {
		opts.FilterPath = cfg.FilterPath
	}
	if !cmd.Flags().Changed("exclude-tags") && cfg.ExcludeTags != "" {
		opts.ExcludeTags = cfg.ExcludeTags
	}
	if !cmd.Flags().Changed("exclude-path") && cfg.ExcludePath != "" {
		opts.ExcludePath = cfg.ExcludePath
	}
	if !cmd.Flags().Changed("create-project-name") && cfg.CreateProjectName != nil {
		opts.CreateProjectName = *cfg.CreateProjectName
	}
	if !cmd.Flags().Changed("create-workspace") && cfg.CreateWorkspace != nil {
		opts.CreateWorkspace = *cfg.CreateWorkspace
	}
	if !cmd.Flags().Changed("execution-order-groups") && cfg.ExecutionOrderGroups != nil {
		opts.ExecutionOrderGroups = *cfg.ExecutionOrderGroups
	}
	if !cmd.Flags().Changed("depends-on") && cfg.DependsOn != nil {
		opts.DependsOn = *cfg.DependsOn
	}
	if !cmd.Flags().Changed("when-modified") && len(cfg.WhenModified) > 0 {
		opts.WhenModified = append([]string(nil), cfg.WhenModified...)
	}
	if !cmd.Flags().Changed("apply-requirements") && len(cfg.ApplyRequirements) > 0 {
		opts.ApplyRequirements = append([]string(nil), cfg.ApplyRequirements...)
	}
	if !cmd.Flags().Changed("tag-workflow") && len(cfg.TagWorkflows) > 0 {
		tags := make([]string, 0, len(cfg.TagWorkflows))
		for tag := range cfg.TagWorkflows {
			tags = append(tags, tag)
		}
		sort.Strings(tags)

		opts.TagWorkflow = make([]string, 0, len(tags))
		for _, tag := range tags {
			opts.TagWorkflow = append(opts.TagWorkflow, fmt.Sprintf("%s=%s", tag, cfg.TagWorkflows[tag]))
		}
	}
	if !cmd.Flags().Changed("tag-requirements") && len(cfg.TagRequirements) > 0 {
		tags := make([]string, 0, len(cfg.TagRequirements))
		for tag := range cfg.TagRequirements {
			tags = append(tags, tag)
		}
		sort.Strings(tags)

		opts.TagRequirements = make([]string, 0, len(tags))
		for _, tag := range tags {
			opts.TagRequirements = append(opts.TagRequirements, fmt.Sprintf("%s=%s", tag, cfg.TagRequirements[tag]))
		}
	}
	if !cmd.Flags().Changed("sort-by") && cfg.SortBy != "" {
		opts.SortBy = cfg.SortBy
	}
	if !cmd.Flags().Changed("project-name-strategy") && cfg.ProjectNameStrategy != "" {
		opts.ProjectNameStrategy = cfg.ProjectNameStrategy
	}
	if !cmd.Flags().Changed("project-name-prefix") && cfg.ProjectNamePrefix != "" {
		opts.ProjectNamePrefix = cfg.ProjectNamePrefix
	}
	if !cmd.Flags().Changed("project-name-depth") && cfg.ProjectNameDepth != nil {
		opts.ProjectNameDepth = *cfg.ProjectNameDepth
	}
	if !cmd.Flags().Changed("format") && cfg.Format != "" {
		opts.Format = cfg.Format
	}

	if len(cfg.Workflows) > 0 {
		opts.ConfigWorkflows = make(map[string]interface{}, len(cfg.Workflows))
		for name, workflow := range cfg.Workflows {
			opts.ConfigWorkflows[name] = workflow
		}
	}

	return nil
}

func parseTagRequirements(entries []string) (map[string][]string, error) {
	mapping := make(map[string][]string, len(entries))

	for _, entry := range entries {
		tag, rawRequirements, ok := strings.Cut(entry, "=")
		if !ok || strings.TrimSpace(tag) == "" || strings.TrimSpace(rawRequirements) == "" {
			return nil, fmt.Errorf("invalid --tag-requirements entry %q, expected tag=req1,req2", entry)
		}

		requirements := strings.Split(rawRequirements, ",")
		cleaned := make([]string, 0, len(requirements))
		for _, requirement := range requirements {
			requirement = strings.TrimSpace(requirement)
			if requirement == "" {
				continue
			}
			cleaned = append(cleaned, requirement)
		}

		if len(cleaned) == 0 {
			return nil, fmt.Errorf("invalid --tag-requirements entry %q, expected at least one requirement", entry)
		}

		mapping[strings.TrimSpace(tag)] = cleaned
	}

	return mapping, nil
}

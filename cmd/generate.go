package cmd

import (
	"bytes"
	"fmt"
	"io"
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
	// WorkflowDisableSafeguards is passed to `terramate run --disable-safeguards`
	// in generated workflows.
	WorkflowDisableSafeguards []string
	// LogLevel comes from the config file only; the --log-level flag is
	// handled by the root command.
	LogLevel             string
	Diff                 bool
	Check                bool
	SortBy               string
	NoHeader             bool
	Format               string
	ExecutionOrderGroups bool
	DependsOn            bool
	ProjectNameStrategy  string
	ProjectNamePrefix    string
	ProjectNameDepth     int
	WhenModified         []string
	ApplyRequirements    []string
	TagWorkflow          []string
	TagRequirements      []string
	ConfigWorkflows      map[string]interface{}
}

const stdoutOutput = "-"

var (
	validFormats        = []string{"yaml", "json"}
	validSortBy         = []string{"dir", "name", "execution_order_group"}
	validNameStrategies = []string{
		atlantis.NameStrategyAutoStrip,
		atlantis.NameStrategyStackName,
		atlantis.NameStrategyLastN,
		atlantis.NameStrategyFull,
	}
)

func newGenerateCmd() *cobra.Command {
	opts := &GenerateOptions{}

	generateCmd := &cobra.Command{
		Use:   "generate",
		Short: "Generate atlantis.yaml from Terramate stacks",
		Long: `Discovers all Terramate stacks in the repository and generates
an atlantis.yaml configuration file. Per-stack customization is
supported via Terramate globals (atlantis_skip, atlantis_workflow, etc).`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runGenerate(cmd, opts)
		},
	}

	flags := generateCmd.Flags()
	flags.StringVar(&opts.Root, "root", ".", "Path to the root directory of the Terramate project")
	flags.StringVar(&opts.Output, "output", "", "Output file path (default: stdout)")
	flags.StringVar(&opts.Config, "config", "", "Path to configuration file (default: .terramate-atlantis.yaml in root)")
	flags.BoolVar(&opts.Autoplan, "autoplan", true, "Enable autoplan for all projects")
	flags.BoolVar(&opts.Parallel, "parallel", true, "Enable parallel plan and apply")
	flags.BoolVar(&opts.Automerge, "automerge", false, "Enable automerge")
	flags.StringVar(&opts.Workflow, "workflow", "", "Default workflow name for all projects")
	flags.StringVar(&opts.TerraformVersion, "terraform-version", "", "Default terraform version for all projects")
	flags.StringVar(&opts.Filter, "filter", "", "Filter stacks by tags (Terramate tag filter syntax)")
	flags.StringVar(&opts.FilterPath, "filter-path", "", "Filter stacks by directory path glob")
	flags.StringVar(&opts.ExcludeTags, "exclude-tags", "", "Exclude stacks by tags (Terramate tag filter syntax)")
	flags.StringVar(&opts.ExcludePath, "exclude-path", "", "Exclude stacks by directory path glob")
	flags.BoolVar(&opts.Changed, "changed", false, "Only include stacks changed from the base ref")
	flags.StringVar(&opts.ChangedBaseRef, "changed-base-ref", "main", "Base git ref for --changed stack detection")
	flags.BoolVar(&opts.CreateProjectName, "create-project-name", true, "Generate project names from stack paths")
	flags.BoolVar(&opts.CreateWorkspace, "create-workspace", false, "Generate workspace values from stack paths")
	flags.BoolVar(&opts.PreserveWorkflows, "preserve-workflows", false, "Preserve workflows from an existing output file")
	flags.BoolVar(&opts.GenerateWorkflows, "generate-workflows", false, "Generate default Atlantis workflows that run via terramate")
	flags.BoolVar(&opts.WorkflowTerramateWrap, "workflow-terramate-wrap", true, "Wrap generated workflow terraform commands with terramate run --no-recursive --")
	flags.StringSliceVar(&opts.WorkflowDisableSafeguards, "workflow-disable-safeguards", atlantis.DefaultDisableSafeguards,
		"Terramate safeguards disabled in generated workflows (comma-separated: "+strings.Join(atlantis.ValidSafeguardKeywords, ", ")+"); pass an empty value to keep all safeguards")
	flags.BoolVar(&opts.Diff, "diff", false, "Print a unified diff between the existing output file and generated content; exits 1 on changes")
	flags.BoolVar(&opts.Check, "check", false, "Exit 1 if the output file differs from generated content; combine with --diff to show the changes")
	flags.StringVar(&opts.SortBy, "sort-by", "dir", "Project sort order: "+strings.Join(validSortBy, ", "))
	flags.BoolVar(&opts.NoHeader, "no-header", false, "Suppress generated file header comment")
	flags.StringVar(&opts.Format, "format", "yaml", "Output format: "+strings.Join(validFormats, ", "))
	flags.BoolVar(&opts.ExecutionOrderGroups, "execution-order-groups", false, "Populate execution_order_group based on stack dependencies")
	flags.BoolVar(&opts.DependsOn, "depends-on", false, "Populate depends_on from stack after/before dependencies")
	flags.StringVar(&opts.ProjectNameStrategy, "project-name-strategy", atlantis.NameStrategyAutoStrip, "Naming strategy: "+strings.Join(validNameStrategies, ", "))
	flags.StringVar(&opts.ProjectNamePrefix, "project-name-prefix", "", "Strip this prefix from stack paths when generating names")
	flags.IntVar(&opts.ProjectNameDepth, "project-name-depth", 3, "Number of trailing path segments for last-n strategy")
	flags.StringSliceVar(&opts.WhenModified, "when-modified", []string{"*.tf", "*.tf.json", ".terraform.lock.hcl"}, "Default when_modified patterns")
	flags.StringSliceVar(&opts.ApplyRequirements, "apply-requirements", nil, "Default apply_requirements entries")
	flags.StringSliceVar(&opts.TagWorkflow, "tag-workflow", nil, "Tag to workflow mapping (format: tag=workflow; comma-separated or repeated)")
	flags.StringArrayVar(&opts.TagRequirements, "tag-requirements", nil, "Tag to requirements mapping (format: tag=req1,req2; repeat the flag for more tags)")

	return generateCmd
}

// validateGenerateOptions checks option values after flags and the config
// file have been merged, so values coming from either source are validated.
func validateGenerateOptions(opts *GenerateOptions) error {
	if !contains(validFormats, opts.Format) {
		return fmt.Errorf("invalid format %q, expected one of: %s", opts.Format, strings.Join(validFormats, ", "))
	}
	if !contains(validSortBy, opts.SortBy) {
		return fmt.Errorf("invalid sort-by %q, expected one of: %s", opts.SortBy, strings.Join(validSortBy, ", "))
	}
	if !contains(validNameStrategies, opts.ProjectNameStrategy) {
		return fmt.Errorf("invalid project-name-strategy %q, expected one of: %s",
			opts.ProjectNameStrategy, strings.Join(validNameStrategies, ", "))
	}
	if err := atlantis.ValidateSafeguards(opts.WorkflowDisableSafeguards); err != nil {
		return fmt.Errorf("--workflow-disable-safeguards: %w", err)
	}
	if (opts.Check || opts.Diff) && writesToStdout(opts.Output) {
		return fmt.Errorf("--check and --diff require an output file (set --output or 'output' in the config file)")
	}
	return nil
}

func contains(values []string, value string) bool {
	for _, v := range values {
		if v == value {
			return true
		}
	}
	return false
}

func writesToStdout(output string) bool {
	return output == "" || output == stdoutOutput
}

func runGenerate(cmd *cobra.Command, opts *GenerateOptions) error {
	stdout := cmd.OutOrStdout()
	stderr := cmd.ErrOrStderr()

	fileCfg, err := loadFileConfig(opts.Root, opts.Config)
	if err != nil {
		return fmt.Errorf("loading config file: %w", err)
	}

	if err := mergeConfig(cmd, opts, fileCfg); err != nil {
		return err
	}

	if err := validateGenerateOptions(opts); err != nil {
		return err
	}

	if opts.LogLevel != "" {
		if err := configureLogging(stderr, opts.LogLevel); err != nil {
			return fmt.Errorf("config file log_level: %w", err)
		}
	}

	warnf := func(format string, args ...any) {
		fmt.Fprintf(stderr, "Warning: "+format+"\n", args...)
	}

	defaultWorkflow := opts.Workflow
	if defaultWorkflow == "" && opts.GenerateWorkflows {
		defaultWorkflow = atlantis.GeneratedWorkflowName
	}

	tagWorkflows, err := parseTagWorkflows(opts.TagWorkflow)
	if err != nil {
		return err
	}

	tagRequirements, err := parseTagRequirements(opts.TagRequirements)
	if err != nil {
		return err
	}

	discovered, err := discovery.Discover(discovery.DiscoverOptions{
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

	cfg, err := atlantis.Generate(discovered.Stacks, atlantis.Options{
		Autoplan:             opts.Autoplan,
		Parallel:             opts.Parallel,
		Automerge:            opts.Automerge,
		DefaultWorkflow:      defaultWorkflow,
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
		NamingStacks:         discovered.AllStacks,
		Warnf:                warnf,
	})
	if err != nil {
		return err
	}

	workflows, err := buildWorkflows(opts, warnf)
	if err != nil {
		return err
	}
	cfg.Workflows = workflows

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

	if opts.Check || opts.Diff {
		return compareWithExisting(stdout, stderr, opts, output)
	}

	if writesToStdout(opts.Output) {
		_, err := stdout.Write(output)
		return err
	}

	if err := os.WriteFile(opts.Output, output, 0o644); err != nil {
		return fmt.Errorf("writing output file: %w", err)
	}

	fmt.Fprintf(stderr, "Generated %s with %d projects\n", opts.Output, len(cfg.Projects))
	return nil
}

// buildWorkflows merges workflow definitions by name, from lowest to highest
// precedence: workflows preserved from the existing output file, then
// generated workflows, then workflows defined in the config file. Generated
// workflows replace stale preserved copies of themselves; hand-written config
// workflows are the most explicit intent, so they can override a generated
// workflow by reusing its name.
func buildWorkflows(opts *GenerateOptions, warnf func(string, ...any)) (map[string]interface{}, error) {
	var preserved map[string]interface{}
	if opts.PreserveWorkflows && !writesToStdout(opts.Output) {
		existing, readErr := os.ReadFile(opts.Output)
		switch {
		case readErr == nil:
			var err error
			preserved, err = atlantis.ExistingWorkflows(existing)
			if err != nil {
				return nil, fmt.Errorf("preserving workflows from %s: %w", opts.Output, err)
			}
		case !os.IsNotExist(readErr):
			return nil, fmt.Errorf("reading existing output file: %w", readErr)
		}
	}

	generated := atlantis.GenerateWorkflows(atlantis.WorkflowOptions{
		Enabled:           opts.GenerateWorkflows,
		WrapWithTerramate: opts.WorkflowTerramateWrap,
		DisableSafeguards: opts.WorkflowDisableSafeguards,
	})

	merged, overrides := atlantis.MergeWorkflows(
		atlantis.WorkflowLayer{Source: atlantis.SourcePreserved, Workflows: preserved},
		atlantis.WorkflowLayer{Source: atlantis.SourceGenerated, Workflows: generated},
		atlantis.WorkflowLayer{Source: atlantis.SourceConfig, Workflows: opts.ConfigWorkflows},
	)
	for _, o := range overrides {
		// Regenerating replaces the previously generated copy; that is the
		// expected path and not worth a warning.
		if o.Winner == atlantis.SourceGenerated && o.Replaced == atlantis.SourcePreserved {
			continue
		}
		warnf("workflow %q from the %s overrides the one from the %s", o.Name, o.Winner, o.Replaced)
	}
	return merged, nil
}

// compareWithExisting implements --check and --diff. --diff prints a unified
// diff to stdout; --check prints a status line to stderr. Either one exits 1
// when the existing file is stale or missing.
func compareWithExisting(stdout, stderr io.Writer, opts *GenerateOptions, generated []byte) error {
	existing, readErr := os.ReadFile(opts.Output)
	if readErr != nil && !os.IsNotExist(readErr) {
		return fmt.Errorf("reading existing output file: %w", readErr)
	}
	missing := os.IsNotExist(readErr)

	if !missing && bytes.Equal(existing, generated) {
		if opts.Check {
			fmt.Fprintf(stderr, "%s is up to date\n", opts.Output)
		}
		return nil
	}

	if opts.Diff {
		diff, _ := atlantis.Diff(existing, generated, filepath.Base(opts.Output))
		fmt.Fprint(stdout, diff)
	}

	if opts.Check {
		state := "is out of date"
		if missing {
			state = "does not exist"
		}
		fmt.Fprintf(stderr, "%s %s, run 'terramate-atlantis-config generate --output %s' to update\n",
			opts.Output, state, opts.Output)
	}

	return &ExitError{Code: 1}
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
			opts.TagRequirements = append(opts.TagRequirements,
				fmt.Sprintf("%s=%s", tag, strings.Join(cfg.TagRequirements[tag], ",")))
		}
	}
	if !cmd.Flags().Changed("changed") && cfg.Changed != nil {
		opts.Changed = *cfg.Changed
	}
	if !cmd.Flags().Changed("changed-base-ref") && cfg.ChangedBaseRef != "" {
		opts.ChangedBaseRef = cfg.ChangedBaseRef
	}
	if !cmd.Flags().Changed("preserve-workflows") && cfg.PreserveWorkflows != nil {
		opts.PreserveWorkflows = *cfg.PreserveWorkflows
	}
	if !cmd.Flags().Changed("generate-workflows") && cfg.GenerateWorkflows != nil {
		opts.GenerateWorkflows = *cfg.GenerateWorkflows
	}
	if !cmd.Flags().Changed("workflow-terramate-wrap") && cfg.WorkflowTerramateWrap != nil {
		opts.WorkflowTerramateWrap = *cfg.WorkflowTerramateWrap
	}
	if !cmd.Flags().Changed("workflow-disable-safeguards") && cfg.WorkflowDisableSafeguards != nil {
		opts.WorkflowDisableSafeguards = append([]string{}, (*cfg.WorkflowDisableSafeguards)...)
	}
	if !cmd.Flags().Changed("no-header") && cfg.NoHeader != nil {
		opts.NoHeader = *cfg.NoHeader
	}
	if !cmd.Flags().Changed("log-level") && cfg.LogLevel != "" {
		opts.LogLevel = cfg.LogLevel
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

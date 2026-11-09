// Package cmd implements the terramate-atlantis-config commands.
package cmd

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
)

const defaultLogLevel = "warn"

var validLogLevels = []string{"trace", "debug", "info", "warn", "error", "disabled"}

// NewRootCmd builds a fresh command tree. Each call returns independent
// commands with their own flag state, which keeps tests isolated.
func NewRootCmd() *cobra.Command {
	var logLevel string

	rootCmd := &cobra.Command{
		Use:   "terramate-atlantis-config",
		Short: "Generate Atlantis config for Terramate projects",
		Long: `Generates an atlantis.yaml configuration file by discovering
Terramate stacks and their metadata using the Terramate SDK.

Similar to terragrunt-atlantis-config, but for Terramate-managed
infrastructure repositories.`,
		// Run prints each error once. Flag errors point to --help (see
		// SetFlagErrorFunc) instead of dumping the usage text.
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			return configureLogging(cmd.ErrOrStderr(), logLevel)
		},
	}

	rootCmd.PersistentFlags().StringVar(&logLevel, "log-level", defaultLogLevel,
		fmt.Sprintf("Terramate SDK log level: %s", strings.Join(validLogLevels, ", ")))

	rootCmd.SetFlagErrorFunc(func(cmd *cobra.Command, err error) error {
		return fmt.Errorf("%w\nRun '%s --help' for usage", err, cmd.CommandPath())
	})

	rootCmd.AddCommand(newGenerateCmd(), newValidateCmd(), newVersionCmd())
	return rootCmd
}

// Run executes the CLI and returns the process exit code. It is the only
// place that prints a returned error.
func Run(args []string, stdout, stderr io.Writer) int {
	rootCmd := NewRootCmd()
	rootCmd.SetArgs(args)
	rootCmd.SetOut(stdout)
	rootCmd.SetErr(stderr)

	err := rootCmd.Execute()
	if err == nil {
		return 0
	}

	var exitErr *ExitError
	if errors.As(err, &exitErr) {
		return exitErr.Code
	}

	fmt.Fprintf(stderr, "Error: %v\n", err)
	return 1
}

func Execute() int {
	return Run(os.Args[1:], os.Stdout, os.Stderr)
}

func configureLogging(w io.Writer, level string) error {
	parsed, err := parseLogLevel(level)
	if err != nil {
		return err
	}

	zerolog.SetGlobalLevel(parsed)
	log.Logger = zerolog.New(zerolog.ConsoleWriter{Out: w, NoColor: true}).With().Timestamp().Logger()
	return nil
}

// parseLogLevel accepts only validLogLevels; zerolog.ParseLevel alone would
// also take fatal, panic and numeric levels.
func parseLogLevel(level string) (zerolog.Level, error) {
	normalized := strings.ToLower(strings.TrimSpace(level))
	for _, valid := range validLogLevels {
		if normalized == valid {
			return zerolog.ParseLevel(normalized)
		}
	}
	return zerolog.NoLevel, fmt.Errorf("invalid --log-level %q, expected one of: %s", level, strings.Join(validLogLevels, ", "))
}

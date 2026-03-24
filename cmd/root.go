package cmd

import (
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "terramate-atlantis-config",
	Short: "Generate Atlantis config for Terramate projects",
	Long: `Generates an atlantis.yaml configuration file by discovering
Terramate stacks and their metadata using the Terramate SDK.

Similar to terragrunt-atlantis-config, but for Terramate-managed
infrastructure repositories.`,
}

// Execute runs the root command.
func Execute() error {
	return rootCmd.Execute()
}

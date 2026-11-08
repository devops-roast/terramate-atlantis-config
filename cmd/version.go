package cmd

import (
	"fmt"
	"runtime/debug"

	"github.com/spf13/cobra"
)

// Version, Commit and BuildDate are set with -ldflags -X by release builds
// (.goreleaser.yaml, Makefile). Other builds fall back to the build
// information the Go toolchain embeds: the module version for
// `go install module@version`, and the VCS revision and time for builds from
// a git checkout.
var (
	Version   = "dev"
	Commit    = "unknown"
	BuildDate = "unknown"
)

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, _ []string) {
			info, _ := debug.ReadBuildInfo()
			version, commit, date := versionInfo(info)
			fmt.Fprintf(cmd.OutOrStdout(), "terramate-atlantis-config %s (commit: %s, built: %s)\n", version, commit, date)
		},
	}
}

// versionInfo returns the version, commit and build date, preferring the
// values set with -ldflags and filling the rest from the embedded build
// information.
func versionInfo(info *debug.BuildInfo) (version, commit, date string) {
	version, commit, date = Version, Commit, BuildDate
	if info == nil {
		return version, commit, date
	}
	if version == "dev" && info.Main.Version != "" && info.Main.Version != "(devel)" {
		version = info.Main.Version
	}
	for _, setting := range info.Settings {
		switch {
		case setting.Key == "vcs.revision" && commit == "unknown":
			commit = setting.Value
		case setting.Key == "vcs.time" && date == "unknown":
			date = setting.Value
		}
	}
	return version, commit, date
}

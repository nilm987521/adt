package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

// Version and BuildTime are set by the main package via ldflags injection
// (e.g. -X main.version=... -X main.buildTime=...) and forwarded here
// if the caller calls SetVersionInfo before Execute().
var (
	Version   = "dev"
	BuildTime = "unknown"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print version information",
	Long:  "Print the version and build time for this adt binary.",
	Run: func(_ *cobra.Command, _ []string) {
		fmt.Printf("adt version %s (built %s)\n", Version, BuildTime)
	},
}

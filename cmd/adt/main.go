// Package main is the entry point for the adt CLI binary.
package main

import "github.com/nilm987521/adt/internal/cli"

// version and buildTime are injected at build time via -ldflags:
//
//	-X main.version=$(git describe --tags --always)
//	-X main.buildTime=$(date -u +%Y-%m-%dT%H:%M:%SZ)
var (
	version   = "dev"
	buildTime = "unknown"
)

func main() {
	cli.Version = version
	cli.BuildTime = buildTime

	cli.Execute()
}

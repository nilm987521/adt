package main

import "github.com/adt-tool/adt/internal/cli"

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

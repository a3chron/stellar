package main

import (
	"github.com/a3chron/stellar/cmd"
	"os"
)

// Version information (set by goreleaser via ldflags)
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	// Pass version info to cmd package
	cmd.SetVersionInfo(version, commit, date)

	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}
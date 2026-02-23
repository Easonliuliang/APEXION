package main

import (
	"runtime/debug"

	"github.com/apexion-ai/apexion/cmd"
)

// version info injected via ldflags:
// go build -ldflags "-X main.version=0.3.1 -X main.commit=abc123 -X main.date=2026-02-19"
var (
	version = "0.3.1"
	commit  = "none"
	date    = "unknown"
)

func init() {
	// Auto-populate commit from Go build info (vcs.revision) when not set via ldflags.
	if commit == "none" {
		if info, ok := debug.ReadBuildInfo(); ok {
			for _, s := range info.Settings {
				if s.Key == "vcs.revision" && len(s.Value) >= 7 {
					commit = s.Value[:7]
					break
				}
			}
		}
	}
}

func main() {
	cmd.Execute(version, commit, date)
}

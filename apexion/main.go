package main

import (
	"github.com/apexion-ai/apexion/cmd"
)

// version info injected via ldflags:
// go build -ldflags "-X main.version=v0.1.0 -X main.commit=abc123 -X main.date=2026-02-19"
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	cmd.Execute(version, commit, date)
}

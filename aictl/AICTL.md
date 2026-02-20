# aictl Project Context

This is the aictl project — an open-source AI coding assistant written in Go.

## Key Facts
- Module path: github.com/aictl/aictl
- Go version: 1.23
- Entry point: main.go → cmd.Execute()

## Rules
- Always run `go build ./...` after modifying any .go file to catch errors early
- Test command: go test ./...
- Do NOT modify the aictl binary directly (it's a compiled artifact)

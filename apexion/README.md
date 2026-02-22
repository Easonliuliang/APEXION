# apexion

> *Immutable Structure, Divine Intelligence.*

**Apex** (pinnacle) + **ion** (energy) — peak intelligence with unstoppable momentum.

AI coding assistant in your terminal. Open-source, model-agnostic.

`apexion` is a pure Go CLI tool that brings an agentic coding experience to your terminal — similar to Claude Code, but open-source and works with any LLM provider.

```
$ apexion
apexion v0.1.0 — deepseek (deepseek-chat)
type your request, /help for commands, /quit to exit
──────────────────────────────────────────────────

> read the main package and tell me what it does
⏺ read_file path="main.go"
  ⎿  (48 lines)
⏺ list_dir path="cmd/"
  ⎿  root.go  run.go  init.go

This is a Go CLI tool that...
```

---

## Features

- **Agentic loop** — LLM plans, calls tools, reads results, iterates until done. No hard iteration cap by default — the model decides when to stop
- **Doom loop protection** — detects when the model repeats the same tool calls and intervenes automatically
- **17 built-in tools** — file read/edit/write, bash, glob, grep, git, web fetch/search, sub-agents, todo tracking, and more
- **MCP protocol** — extend with any [Model Context Protocol](https://modelcontextprotocol.io/) server
- **Streaming TUI** — real-time bubbletea terminal UI with markdown rendering, tool call display, and spinner animations
- **Model-agnostic** — Anthropic, OpenAI, DeepSeek, Qwen, Kimi, GLM, Doubao, Groq, Ollama, or any OpenAI-compatible API
- **Permission system** — interactive, auto-approve, or yolo mode with session-level approval memory
- **Session management** — save, resume, and list sessions. Auto-compaction keeps long conversations within context limits
- **Cross-session memory** — `/memory add` to persist knowledge across sessions
- **Custom commands** — define reusable prompt templates as markdown files
- **Project context** — reads `APEXION.md` to understand your project's conventions
- **Single binary** — `go build` produces one self-contained executable
- **Cross-platform** — macOS, Linux, Windows

---

## Quick Start

```bash
# Install
go install github.com/apexion-ai/apexion@latest

# Configure (interactive wizard)
apexion init

# Start chatting
apexion
```

Or with environment variables:

```bash
export LLM_API_KEY=your-key
export LLM_BASE_URL=https://api.deepseek.com   # omit for OpenAI
export LLM_MODEL=deepseek-chat

apexion
```

---

## Installation

### Go (recommended)

```bash
go install github.com/apexion-ai/apexion@latest
```

### Build from source

```bash
git clone https://github.com/apexion-ai/apexion
cd apexion
go build -o apexion .
```

### Pre-built binaries

Download from [GitHub Releases](https://github.com/apexion-ai/apexion/releases) for macOS, Linux, and Windows (amd64 / arm64).

---

## Configuration

### Config file

`apexion init` creates `~/.config/apexion/config.yaml`:

```yaml
provider: deepseek
model: deepseek-chat

providers:
  anthropic:
    api_key: sk-ant-...
    model: claude-opus-4-5

  deepseek:
    api_key: sk-...
    model: deepseek-chat

  openai:
    api_key: sk-...
    model: gpt-4o

  qwen:
    api_key: sk-...
    base_url: https://dashscope.aliyuncs.com/compatible-mode/v1
    model: qwen-max

# 0 = unlimited (default). Loop exits when model stops calling tools.
# Set to a positive number as a safety cap.
max_iterations: 0

# Override provider's default context window size. 0 = use provider default.
context_window: 0

permissions:
  mode: interactive          # interactive | auto-approve | yolo
  auto_approve_tools:
    - read_file
    - glob
    - grep
    - list_dir
    - web_fetch
    - web_search
  allowed_commands:          # bash command whitelist (prefix match)
    - go test
    - go build
  denied_commands:           # blacklist (enforced even in yolo mode)
    - rm -rf /

web:
  search_provider: tavily    # tavily | exa | jina (free, no key)
  search_api_key: tvly-...
```

### Environment variables

| Variable | Description |
|----------|-------------|
| `LLM_API_KEY` | API key for the current provider |
| `LLM_BASE_URL` | Base URL (for OpenAI-compatible providers) |
| `LLM_MODEL` | Model override |
| `ANTHROPIC_API_KEY` | Anthropic API key |
| `APEXION_PROVIDER` | Provider selection (`deepseek`, `anthropic`, etc.) |
| `APEXION_MODEL` | Model selection |
| `TAVILY_API_KEY` | Tavily search API key |
| `EXA_API_KEY` | Exa search API key |

### Project context — APEXION.md

Create `APEXION.md` in your project root to give apexion persistent knowledge about your project:

```markdown
# My Project

## Rules
- Test command: make test
- Never modify files under vendor/
- Commit messages must be in English
- Entry point is cmd/server/main.go
```

apexion loads this file automatically and injects it into the system prompt. Also supports `~/.config/apexion/context.md` for global preferences.

---

## Usage

### Interactive mode (default)

```bash
apexion
```

Type any natural language request. apexion will plan and execute using its tools.

### Non-interactive mode

```bash
apexion run -P "add error handling to the login function in auth/handler.go"
apexion run -P "run the tests and fix any failures"
```

### CLI flags

```
apexion [flags]

Flags:
  -p, --provider string   Override provider (deepseek, anthropic, openai, ...)
  -m, --model string      Override model
  -c, --config string     Config file path (default ~/.config/apexion/config.yaml)
      --auto-approve      Skip all tool execution confirmations
      --max-turns int     Max agent loop iterations (0=unlimited, default 0)
      --tui               Force bubbletea TUI mode (auto-detected by default)
```

### Slash commands

| Command | Description |
|---------|-------------|
| `/help` | Show all available commands |
| `/model <name>` | Switch model at runtime |
| `/provider <name>` | Switch provider at runtime |
| `/config` | Show current configuration |
| `/plan` | Toggle plan mode (read-only analysis) |
| `/compact` | Manually trigger context compaction |
| `/changes` | Show files modified in this session |
| `/trust` | Show session-level tool approvals |
| `/trust reset` | Clear all session approvals |
| `/rules` | List loaded rules |
| `/skills` | List available skills |
| `/memory` | List saved memories |
| `/memory add <text>` | Save a memory (use `#tag` to add tags) |
| `/memory search <q>` | Search memories |
| `/memory delete <id>` | Delete a memory |
| `/mcp` | Show MCP server connection status |
| `/mcp reset` | Reconnect all MCP servers |
| `/commands` | List custom commands |
| `/audit` | Show bash command audit log |
| `/save` | Save current session |
| `/sessions` | List saved sessions |
| `/resume <id>` | Resume a saved session (short ID prefix) |
| `/history` | Show message history |
| `/cost` | Show token usage |
| `/clear` | Clear conversation history |
| `/quit` | Save and exit |

---

## Built-in Tools

| Tool | Permission | Description |
|------|-----------|-------------|
| `read_file` | Auto | Read file contents with line numbers, supports offset/limit |
| `edit_file` | Ask | Exact string replace — precise, no diff parsing |
| `write_file` | Ask | Create new files (parent dirs created automatically) |
| `bash` | Ask | Execute shell commands with timeout (supports background mode) |
| `glob` | Auto | Find files by glob pattern (e.g., `**/*.go`) |
| `grep` | Auto | Search file contents by regex with glob filtering |
| `list_dir` | Auto | List directory contents with sizes |
| `git_status` | Auto | Show working tree status |
| `git_diff` | Auto | Show changes (staged/unstaged) |
| `git_commit` | Ask | Stage files and commit |
| `git_push` | Confirm | Push to remote (requires explicit confirmation) |
| `web_fetch` | Auto | Fetch web page and convert to markdown (15-min cache) |
| `web_search` | Auto | Web search via Tavily, Exa, or Jina |
| `task` | Auto | Launch read-only sub-agent for research tasks |
| `todo_write` | Auto | Create/update todo list for multi-step tasks |
| `todo_read` | Auto | Read current todo list |
| `question` | Auto | Ask user clarifying questions with options |

**Permission levels:**
- **Auto** — executed immediately (read-only operations)
- **Ask** — terminal prompt `[y/N]` before execution
- **Confirm** — prominent warning before execution (destructive operations)

---

## MCP (Model Context Protocol)

apexion supports MCP servers for extensibility. Create `~/.config/apexion/mcp.json` or `.apexion/mcp.json` in your project:

```json
{
  "mcpServers": {
    "filesystem": {
      "command": "npx",
      "args": ["-y", "@modelcontextprotocol/server-filesystem", "/tmp"],
      "env": { "KEY": "${ENV_VAR}" }
    },
    "remote": {
      "url": "https://mcp.example.com/sse"
    }
  }
}
```

Supports both **stdio** (child process) and **HTTP** (streamable) transports. Project-level config overrides global config. Use `/mcp` to check connection status.

### Recommended MCP Servers

#### Context7 — Up-to-date library documentation

Provides current documentation for popular libraries, avoiding hallucinated or outdated API usage.

```json
{
  "mcpServers": {
    "context7": {
      "command": "npx",
      "args": ["-y", "@upstash/context7-mcp@latest"]
    }
  }
}
```

#### Sequential Thinking — Complex problem solving

Helps with multi-step reasoning and complex architectural decisions.

```json
{
  "mcpServers": {
    "sequential-thinking": {
      "command": "npx",
      "args": ["-y", "@anthropic/sequential-thinking-mcp@latest"]
    }
  }
}
```

#### Filesystem — Sandbox file access

Provides sandboxed file access to a specific directory, useful for restricting operations.

```json
{
  "mcpServers": {
    "filesystem": {
      "command": "npx",
      "args": ["-y", "@modelcontextprotocol/server-filesystem", "/path/to/allowed/dir"]
    }
  }
}
```

---

## Custom Commands

Define reusable prompt templates as markdown files in `.apexion/commands/` or `~/.config/apexion/commands/`:

```markdown
---
name: review
description: Code review for a file
args:
  - name: file
    required: true
---
Review {{.file}} for bugs, security issues, and style problems.
Focus on correctness first, then readability.
```

Then use it: `/review src/auth/handler.go`

List all custom commands with `/commands`.

---

## Rules

Define modular rules in `.apexion/rules/` or `~/.config/apexion/rules/` as markdown files:

```markdown
---
description: "Go coding conventions"
---
- Use snake_case for file names
- Always run `go vet` before committing
- Prefer table-driven tests
```

Rules can optionally include `path_patterns` in frontmatter for scoped activation:

```markdown
---
description: "Frontend rules"
path_patterns:
  - "*.tsx"
  - "src/components/**"
---
Use functional components with hooks.
```

Rules without `path_patterns` are always active. Use `/rules` to list loaded rules.

---

## Skills

Place domain knowledge files in `.apexion/skills/` or `~/.config/apexion/skills/` as markdown files. The LLM is informed of available skills and can load them on demand using `read_file`.

```
.apexion/skills/
  go-patterns.md      # Go coding patterns for this project
  api-design.md       # API design conventions
  deployment.md       # Deployment procedures
```

Use `/skills` to list available skills.

---

## Plan Mode

Toggle plan mode with `/plan`. In plan mode:

- Only read-only tools are available (no file modifications, no bash)
- The LLM analyzes and proposes a plan instead of executing
- The status bar shows a "PLAN" indicator

Use this to review the agent's approach before allowing execution.

---

## Sandbox

Configure bash tool restrictions in `config.yaml`:

```yaml
sandbox:
  work_dir: /path/to/project    # restrict bash to this directory
  audit_log: /tmp/apexion.log   # log all bash commands
```

Use `/audit` to view recent bash commands from the audit log.

---

## Doom Loop Detection

When the agent runs without an iteration cap (default), a built-in doom loop detector prevents infinite loops:

- **Warning (3x)** — if the model issues the same tool calls 3 times in a row, a hint is injected into the conversation asking it to try a different approach
- **Stop (5x)** — if the same tool calls repeat 5 times, the loop is force-stopped

You can also set a hard cap via config (`max_iterations: 30`) or CLI flag (`--max-turns 30`) as an additional safety valve.

---

## Supported Providers

### International

| Provider | Config key | Notes |
|----------|-----------|-------|
| **Anthropic** | `anthropic` | Claude Opus, Sonnet, Haiku (native API) |
| **OpenAI** | `openai` | GPT-4o, o1, etc. |
| **Groq** | `groq` | Fast inference, Llama models |
| **Ollama** | `ollama` | Local models |

### Chinese models (OpenAI-compatible)

| Provider | Config key | Recommended model |
|----------|-----------|------------------|
| **DeepSeek** | `deepseek` | `deepseek-chat` |
| **Qwen (Alibaba)** | `qwen` | `qwen-max` |
| **Kimi (Moonshot)** | `kimi` | `moonshot-v1-128k` |
| **GLM (Zhipu)** | `glm` | `glm-4` |
| **Doubao (ByteDance)** | `doubao` | `doubao-pro-32k` |
| **MiniMax** | `minimax` | `abab6.5-chat` |

All OpenAI-compatible providers share the same adapter — only `api_key` and `base_url` differ.

---

## Architecture

```
apexion/
├── main.go                    # Entry point
├── cmd/                       # CLI commands (cobra)
│   ├── root.go                # Global flags, provider setup
│   ├── chat.go                # Interactive mode
│   ├── run.go                 # Non-interactive mode
│   └── init.go                # Config wizard
└── internal/
    ├── agent/                 # Agentic loop + REPL
    │   ├── agent.go           # Core agent, slash commands, system prompt
    │   ├── loop.go            # LLM → tool → result → repeat
    │   ├── doomloop.go        # Doom loop detection
    │   ├── commands.go        # Custom commands loader
    │   └── prompts/           # Modular system prompt sections
    ├── provider/              # LLM adapters
    │   ├── provider.go        # Unified interface + event types
    │   ├── openai.go          # OpenAI-compatible adapter
    │   └── anthropic.go       # Anthropic native adapter
    ├── tools/                 # 17 tool implementations
    ├── tui/                   # Bubbletea TUI + plain IO
    ├── session/               # Conversation history, memory, compaction
    ├── permission/            # Permission policy + approval memory
    ├── mcp/                   # MCP client + config loader
    └── config/                # Config loading (YAML + env vars)
```

The provider interface emits a unified `Event` stream (`TextDelta`, `ToolCallDone`, `Done`, `Error`), isolating the agentic loop from provider-specific streaming formats.

---

## Contributing

Contributions are welcome. Please read [CONTRIBUTING.md](CONTRIBUTING.md) before submitting a PR.

```bash
git clone https://github.com/apexion-ai/apexion
cd apexion
go build ./...
go test ./...
```

---

## License

Apache License 2.0 — see [LICENSE](LICENSE).

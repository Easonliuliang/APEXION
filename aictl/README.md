# aictl

**AI coding assistant in your terminal. Open-source, model-agnostic.**

`aictl` is a pure Go CLI tool that brings an agentic coding experience to your terminal — similar to Claude Code, but open-source and works with any LLM provider.

```
$ aictl
aictl — type your request, /quit to exit
--------------------------------------------------

> read the main package and tell me what it does
  Executing read_file...
  Executing list_dir...

This is a Go CLI tool that...
```

---

## Features

- **Agentic loop** — LLM plans, calls tools, reads results, iterates until done
- **Streaming output** — responses appear in real-time as the LLM generates them
- **Model-agnostic** — works with Anthropic, OpenAI, DeepSeek, Qwen, Kimi, GLM, and more
- **7+ built-in tools** — read/edit/write files, bash, glob, grep, git operations
- **Permission system** — confirms before writing files or running commands
- **Project context** — reads `AICTL.md` to understand your project's conventions
- **Single binary** — `go build` produces one self-contained executable, no runtime deps
- **Cross-platform** — macOS, Linux, Windows

---

## Quick Start

```bash
# Install
go install github.com/aictl/aictl@latest

# Configure (interactive wizard)
aictl init

# Start chatting
aictl
```

Or with environment variables:

```bash
export LLM_API_KEY=your-key
export LLM_BASE_URL=https://api.deepseek.com   # omit for OpenAI
export LLM_MODEL=deepseek-chat

aictl
```

---

## Installation

### Go (recommended)

```bash
go install github.com/aictl/aictl@latest
```

### Build from source

```bash
git clone https://github.com/aictl/aictl
cd aictl
go build -o aictl .
```

### Pre-built binaries

Download from [GitHub Releases](https://github.com/aictl/aictl/releases) for macOS, Linux, and Windows (amd64 / arm64).

---

## Configuration

### Config file

`aictl init` creates `~/.config/aictl/config.yaml`:

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

permissions:
  mode: interactive          # interactive | auto-approve
  auto_approve_tools:
    - read_file
    - glob
    - grep
    - list_dir
    - git_status
    - git_diff
```

### Environment variables

| Variable | Description |
|----------|-------------|
| `LLM_API_KEY` | API key for the current provider |
| `LLM_BASE_URL` | Base URL (for OpenAI-compatible providers) |
| `LLM_MODEL` | Model override |
| `ANTHROPIC_API_KEY` | Anthropic API key |
| `AICTL_PROVIDER` | Provider selection (`deepseek`, `anthropic`, etc.) |
| `AICTL_MODEL` | Model selection |

### Project context — AICTL.md

Create `AICTL.md` in your project root to give aictl persistent knowledge about your project:

```markdown
# My Project

## Rules
- Test command: make test
- Never modify files under vendor/
- Commit messages must be in English
- Entry point is cmd/server/main.go
```

aictl loads this file automatically and injects it into the system prompt. Also supports `~/.config/aictl/context.md` for global preferences.

---

## Usage

### Interactive mode (default)

```bash
aictl
```

Type any natural language request. aictl will plan and execute using its tools.

### Non-interactive mode

```bash
aictl run -P "add error handling to the login function in auth/handler.go"
aictl run -P "run the tests and fix any failures"
```

### CLI flags

```
aictl [flags]

Flags:
  -p, --provider string   Override provider (deepseek, anthropic, openai, ...)
  -m, --model string      Override model
  -c, --config string     Config file path (default ~/.config/aictl/config.yaml)
      --auto-approve      Skip all tool execution confirmations
```

### Slash commands

| Command | Description |
|---------|-------------|
| `/clear` | Clear conversation history |
| `/history` | Show message history |
| `/cost` | Show token usage for this session |
| `/quit` | Exit |

---

## Supported Providers

### International

| Provider | Config key | Notes |
|----------|-----------|-------|
| **Anthropic** | `anthropic` | Claude Opus, Sonnet, Haiku |
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

## Built-in Tools

| Tool | Permission | Description |
|------|-----------|-------------|
| `read_file` | Auto | Read file contents with line numbers, supports offset/limit |
| `edit_file` | Ask | Exact string replace — precise, no diff parsing |
| `write_file` | Ask | Create new files |
| `bash` | Ask | Execute shell commands with timeout |
| `glob` | Auto | Find files by pattern |
| `grep` | Auto | Search file contents by regex |
| `list_dir` | Auto | List directory contents |
| `git_status` | Auto | Show working tree status |
| `git_diff` | Auto | Show changes (staged/unstaged) |
| `git_commit` | Ask | Stage files and commit |
| `git_push` | Confirm | Push to remote |

**Permission levels:**
- **Auto** — executed immediately (read-only operations)
- **Ask** — terminal prompt `[y/N]` before execution
- **Confirm** — prominent warning before execution

---

## Architecture

```
aictl/
├── main.go                    # Entry point
├── cmd/                       # CLI commands (cobra)
│   ├── root.go                # Global flags, provider setup
│   ├── chat.go                # Interactive mode
│   └── run.go                 # Non-interactive mode
└── internal/
    ├── agent/                 # Agentic loop + REPL
    │   ├── agent.go           # Core agent, system prompt
    │   ├── loop.go            # LLM → tool → result → repeat
    │   └── context.go         # AICTL.md loader
    ├── provider/              # LLM adapters
    │   ├── provider.go        # Unified interface + event types
    │   ├── openai.go          # OpenAI-compatible adapter
    │   └── anthropic.go       # Anthropic native adapter
    ├── tools/                 # Tool implementations
    ├── session/               # Conversation history
    ├── permission/            # Permission policy
    └── config/                # Config loading (yaml + env vars)
```

The provider interface emits a unified `Event` stream (`TextDelta`, `ToolCallDone`, `Done`, `Error`), isolating the agentic loop from provider-specific streaming formats.

---

## Contributing

Contributions are welcome. Please read [CONTRIBUTING.md](CONTRIBUTING.md) before submitting a PR.

```bash
git clone https://github.com/aictl/aictl
cd aictl
go build ./...
go test ./...
```

---

## License

Apache License 2.0 — see [LICENSE](LICENSE).

> **Note:** `aictl` is an independent open-source project and is not affiliated with Anthropic.

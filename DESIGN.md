# aictl — 设计文档

> **项目工作名**：`aictl`（原提案名 `claude-agent-go` 存在商标风险，见第 1 节）
>
> 本文档是基于对原始提示词的深度分析后产出的项目设计蓝图，覆盖架构、技术选型、接口设计、开源规范和实施路线。

---

## 目录

1. [项目命名与法律风险](#1-项目命名与法律风险)
2. [项目概述与目标](#2-项目概述与目标)
3. [技术选型决策](#3-技术选型决策)
4. [原始提示词的问题与修正](#4-原始提示词的问题与修正)
5. [推荐项目目录结构](#5-推荐项目目录结构)
6. [核心接口设计](#6-核心接口设计)
7. [Agent 主循环设计](#7-agent-主循环设计)
8. [工具系统设计](#8-工具系统设计)
9. [Provider 抽象层设计](#9-provider-抽象层设计)
10. [权限系统设计](#10-权限系统设计)
11. [会话与上下文管理](#11-会话与上下文管理)
12. [TUI 层设计](#12-tui-层设计)
13. [配置系统设计](#13-配置系统设计)
14. [开源规范](#14-开源规范)
15. [CI/CD 与发布策略](#15-cicd-与发布策略)
16. [开发路线图](#16-开发路线图)
17. [参考项目](#17-参考项目)

---

## 1. 项目命名与法律风险

### 1.1 "claude-agent-go" 的商标风险

**结论：高风险，强烈建议更名。**

"CLAUDE" 是 Anthropic, PBC 在美国专利商标局（USPTO）的注册商标（申请号 97790228），覆盖 AI 软件类别。Anthropic 近年来积极执法，已有多个第三方项目因此受到限制。将竞争性开源工具命名为 "claude-agent-go" 将大大增加法律审查风险，且可能面临 GitHub DMCA 投诉和仓库下架。

### 1.2 推荐名称

| 候选名 | 风格 | 说明 |
|--------|------|------|
| **aictl** ⭐ | Unix 风格 | 类比 kubectl/systemctl，简洁，暗示"AI 控制工具" |
| shellmind | 描述性 | Shell + Mind，直觉明确 |
| agentpilot | 描述性 | Agent 引航员 |
| goagent-cli | 直白 | Go + Agent + CLI |
| codeweave | 隐喻 | 编织代码 |

**首选推荐：`aictl`**
- 极简，4 个字母
- 无任何商标冲突
- Unix 命名哲学（对比 kubectl, systemctl, crictl）
- 域名 aictl.dev 可注册

---

## 2. 项目概述与目标

### 2.1 定位

`aictl` 是一个纯 Go 语言编写的开源 CLI 工具，目标是：

- **体验对标** Anthropic 官方 Claude Code CLI（终端交互式 AI 编码代理）
- **完全开源**，Apache 2.0 协议，可自由修改和商用
- **Model-agnostic**：默认 Anthropic Claude，通过配置支持 OpenAI、Google Gemini、Groq、Ollama 等
- **单一二进制**：`go build` 后无外部依赖，跨平台（macOS/Linux/Windows）
- **安全优先**：工具执行前默认询问用户确认，支持精细化权限控制

### 2.2 核心功能

```
用户输入自然语言指令
    ↓
LLM 规划（reasoning + tool call 决策）
    ↓
工具执行（ReadFile / EditFile / BashExec / Search / Git）
    ↓
结果反馈给 LLM
    ↓
迭代直至完成任务或用户满意
```

### 2.3 非目标（明确不做）

- **不做 Web UI**：纯终端 CLI，不提供 HTTP 接口
- **不做 RAG pipeline**：不集成向量数据库，不做检索增强生成
- **不做代码执行沙箱**：BashExec 直接在用户 shell 中执行，用权限系统保障安全
- **不做 LangChain 替代品**：不提供 Chain/Graph 等复杂 workflow 编排
- **不做多模态**：v1.0 不支持图片输入（可作为后续扩展）

---

## 3. 技术选型决策

### 3.1 选型总表

| 层 | 选型 | 否决方案 | 决策理由 |
|----|------|---------|---------|
| **CLI 框架** | `spf13/cobra` | urfave/cli v2/v3 | 行业标准；子命令+全局flags+自动补全；OpenCode 验证 |
| **TUI 框架** | `charmbracelet/bubbletea` | tcell, tview | Elm Architecture 天然适配流式输出；唯一成熟方案 |
| **Markdown 渲染** | `charmbracelet/glamour` | 无替代 | Go 生态唯一成熟终端 Markdown 渲染库 |
| **终端样式** | `charmbracelet/lipgloss` | 手写 ANSI | CSS-like API，与 bubbletea 无缝集成 |
| **Anthropic SDK** | `github.com/anthropics/anthropic-sdk-go` | 自写 HTTP | 官方维护，类型安全，streaming 支持完整 |
| **OpenAI SDK** | `github.com/openai/openai-go` | go-openai | 官方 SDK（2024年底发布），覆盖 OpenAI/Groq/Ollama |
| **Gemini SDK** | `google.golang.org/genai` | Genkit | 官方 SDK，直接，无多余抽象 |
| **LLM 中间层** | 自实现 thin adapter | langchaingo, genkit | 最小依赖；避免过度抽象；OpenCode 验证 |
| **国产模型** | OpenAI adapter 复用（配置不同 base_url） | 独立 adapter | 绝大多数国产模型已提供 OpenAI 兼容 API |
| **Git 操作** | `os/exec` 调用 git | go-git | 性能、SSH 兼容性、hooks 支持；用户已安装 git |
| **配置** | `gopkg.in/yaml.v3` + struct | viper | 最小依赖；项目配置项少，yaml.v3 足够 |
| **会话持久化** | SQLite (`ncruces/go-sqlite3`) | 纯 JSON 文件 | 支持查询、事务；go-sqlite3 无 CGO |
| **MCP 支持** | `mark3labs/mcp-go` | 自实现 | Go MCP 事实标准 |
| **日志** | `log/slog` (标准库) | zap, zerolog | 标准库，Go 1.21+，零依赖 |

### 3.2 关键决策详述

#### 为什么不用 langchaingo / genkit

langchaingo 的问题：
1. 依赖极重：引入 20+ 传递依赖（向量数据库驱动、embedding 库等），违背"单一二进制、最小依赖"原则
2. 抽象层厚：Chain/Agent/Memory/Retriever 模型为 RAG pipeline 设计，不适合 coding agent 的 tool-calling loop
3. Go 版本是 JS 版的移植，抽象模型并非为 Go 原生设计

Genkit 的问题：
- 偏向 Google Cloud 生态，作为 model-agnostic 工具的底层不够中立

**参考验证**：OpenCode（目前最相似的开源项目）直接使用三个官方 SDK + 自实现 adapter，完全没有引入 langchaingo 或 genkit。

#### 为什么不用 go-git

| 对比维度 | go-git | exec git |
|---------|--------|----------|
| 大仓 diff 性能 | 比 git 慢 2 个数量级 | 原生性能 |
| SSH push 认证 | 需要手动处理 SSH agent/keyfile | 自动继承系统配置 |
| git hooks 支持 | **不支持** | 完全支持 |
| .gitconfig 兼容性 | 不完全 | 完全 |
| 安装依赖 | 无需 git binary | 用户已安装（100%） |

结论：go-git 的唯一优势（无需 git binary）在目标用户（开发者）场景下不存在。

---

## 4. 原始提示词的问题与修正

### 4.1 矛盾点

| # | 原始描述 | 问题 | 修正方案 |
|---|---------|------|---------|
| 1 | "依赖最小化" + 推荐 genkit/langchaingo | 两者直接矛盾。langchaingo 是依赖最重的选项之一 | 自实现 thin adapter + 官方 SDK |
| 2 | "用 go-git 避免 exec git" | 目标用户是开发者（100% 已装 git），go-git 性能差、SSH 复杂 | 直接 exec git，封装约 100 行 wrapper |
| 3 | 强调可测试性但无测试策略 | 缺少 mock 方案、接口隔离策略 | Provider/Tool 均通过接口隔离，测试层注入 mock |

### 4.2 描述不清晰

| # | 原始描述 | 问题 | 修正方案 |
|---|---------|------|---------|
| 1 | `EditFile(path, instruction or diff)` | "instruction or diff" 严重歧义。LLM 生成 diff 极不可靠 | 采用 exact string replace（old_string → new_string），参见第 8.2 节 |
| 2 | 权限系统"危险操作询问确认" | 未定义操作分级、白名单粒度、会话级 vs 一次性许可 | 三级白名单设计，参见第 10 节 |
| 3 | "system prompt 可自定义" | 未说明内容策略、prompt injection 防护、project context 注入 | 参见第 7.2 节 |
| 4 | 未提及 streaming | 流式输出是核心 UX，完全没有描述 | streaming-first 设计，贯穿 Provider/Agent/TUI 各层 |

### 4.3 缺失的重要功能

| # | 缺失功能 | 重要性 | 补充说明 |
|---|---------|--------|---------|
| 1 | **Context Window 管理** | 关键 | 无此功能，长会话必然崩溃 |
| 2 | **TUI 层** | 重要 | 纯 fmt.Print 无法构建专业 CLI 体验 |
| 3 | **Token 计数与成本显示** | 重要 | 用户需要了解消耗情况 |
| 4 | **错误恢复与重试** | 重要 | API rate limit / 网络错误处理 |
| 5 | **Graceful Shutdown** | 中等 | Ctrl+C 时保存会话、清理资源 |
| 6 | **MCP 支持** | 中等 | Model Context Protocol，扩展工具生态 |
| 7 | **Agent Loop 循环上限** | 中等 | 防止 LLM 陷入无限工具调用 |

---

## 5. 推荐项目目录结构

```
aictl/
├── main.go                         # 入口，仅调用 cmd.Execute()
├── go.mod
├── go.sum
├── LICENSE                         # Apache 2.0
├── README.md
├── CONTRIBUTING.md
├── SECURITY.md
├── CODE_OF_CONDUCT.md
├── CHANGELOG.md
├── Makefile                        # build/test/lint 快捷命令
├── .goreleaser.yaml
├── .golangci.yml
├── .gitignore
│
├── .github/
│   ├── ISSUE_TEMPLATE/
│   │   ├── bug_report.yml
│   │   └── feature_request.yml
│   ├── PULL_REQUEST_TEMPLATE.md
│   └── workflows/
│       ├── ci.yml                  # lint + test + build matrix
│       └── release.yml             # tag 触发 GoReleaser
│
├── cmd/
│   ├── root.go                     # 根命令，全局 flags，配置加载
│   ├── chat.go                     # 默认交互模式
│   ├── run.go                      # 非交互模式（-p "prompt" flag）
│   ├── config.go                   # 配置查看/修改命令
│   ├── version.go                  # 版本信息
│   └── init.go                     # 初始化配置文件
│
├── internal/
│   │
│   ├── agent/
│   │   ├── agent.go                # Agent 核心结构与 Run() 入口
│   │   ├── loop.go                 # Agentic Loop 主循环
│   │   ├── state.go                # 状态机定义
│   │   └── agent_test.go
│   │
│   ├── provider/                   # LLM Provider 抽象层（原提示词中的 internal/llm/）
│   │   ├── provider.go             # Provider 接口 + 统一事件类型定义
│   │   ├── message.go              # Message/Content/ToolCall 统一类型
│   │   ├── anthropic.go            # Anthropic adapter（基于 anthropic-sdk-go）
│   │   ├── anthropic_test.go
│   │   ├── openai.go               # OpenAI adapter（覆盖 OpenAI/Groq/Ollama）
│   │   ├── gemini.go               # Google Gemini adapter
│   │   └── registry.go             # Provider 注册表（按名字查找）
│   │
│   ├── tools/
│   │   ├── tool.go                 # Tool 接口定义
│   │   ├── registry.go             # ToolRegistry
│   │   ├── executor.go             # ToolExecutor（超时、取消、输出截断）
│   │   ├── readfile.go             # ReadFile 工具
│   │   ├── editfile.go             # EditFile（exact string replace）
│   │   ├── writefile.go            # WriteFile（新建文件）
│   │   ├── bash.go                 # BashExec 工具
│   │   ├── glob.go                 # GlobSearch 工具
│   │   ├── grep.go                 # GrepSearch 工具
│   │   ├── listdir.go              # ListDirectory 工具
│   │   ├── git.go                  # Git 工具（exec git wrapper）
│   │   └── fetch.go                # WebFetch 工具（可选）
│   │
│   ├── tui/
│   │   ├── app.go                  # bubbletea App Model（顶层状态机）
│   │   ├── chat.go                 # 对话视图（流式输出渲染）
│   │   ├── input.go                # 多行输入组件
│   │   ├── confirm.go              # 权限确认对话框
│   │   ├── spinner.go              # 加载状态指示器
│   │   ├── markdown.go             # glamour Markdown 渲染封装
│   │   └── theme.go                # lipgloss 主题定义
│   │
│   ├── session/
│   │   ├── session.go              # 会话生命周期管理
│   │   ├── history.go              # 消息历史（读/写/查询）
│   │   ├── context.go              # Context Window 管理、消息裁剪
│   │   ├── token.go                # Token 计数与预算管理
│   │   └── storage.go              # SQLite 存储层
│   │
│   ├── permission/
│   │   ├── permission.go           # PermissionPolicy 接口
│   │   ├── policy.go               # DefaultPolicy 实现
│   │   └── prompt.go               # 终端确认交互
│   │
│   ├── sandbox/
│   │   ├── process.go              # 子进程执行（超时、信号、输出捕获）
│   │   └── shell.go                # Shell 命令构建与验证
│   │
│   └── config/
│       ├── config.go               # Config struct + 加载逻辑
│       ├── defaults.go             # 默认值
│       └── config_test.go
│
├── docs/
│   ├── architecture.md             # 架构概览（本文档精简版）
│   └── adr/                        # 架构决策记录
│       ├── 001-use-cobra.md
│       ├── 002-thin-provider-adapter.md
│       └── 003-exec-git-not-go-git.md
│
├── examples/
│   └── custom-tool/                # 如何添加自定义工具的示例
│
├── scripts/
│   ├── install.sh                  # curl 一键安装脚本
│   └── dev-setup.sh                # 开发环境初始化
│
└── testdata/
    └── sample-project/             # 集成测试用的测试项目
```

**关键变更说明（对比原始提示词）**：
- `internal/llm/` → `internal/provider/`：语义更清晰，provider 是行业通用术语
- 新增 `internal/tui/`：TUI 是独立关注点
- 新增 `internal/session/`：会话和 token 管理是核心功能
- 新增 `internal/permission/`：权限系统独立模块
- 新增 `internal/sandbox/`：进程执行隔离
- 无 `pkg/`：初期无外部导出需求，保持简单

---

## 6. 核心接口设计

### 6.1 Provider 接口

```go
// Provider 是所有 LLM 提供商的统一接口
type Provider interface {
    // Chat 发起对话，返回 streaming 事件 channel
    // channel 关闭时表示本次响应结束
    Chat(ctx context.Context, req *ChatRequest) (<-chan StreamEvent, error)

    // Name 返回 provider 标识符，如 "anthropic", "openai", "gemini"
    Name() string

    // Models 返回该 provider 支持的模型列表
    Models() []string

    // DefaultModel 返回该 provider 的默认模型
    DefaultModel() string
}

// ChatRequest 统一请求格式
type ChatRequest struct {
    Model        string
    Messages     []Message
    Tools        []ToolSchema    // 从 ToolRegistry 转换而来
    SystemPrompt string
    MaxTokens    int
    Temperature  float64
}

// StreamEvent 统一流式事件
type StreamEvent struct {
    Type EventType

    // EventTextDelta
    TextDelta string

    // EventToolCallStart
    ToolCallID   string
    ToolCallName string

    // EventToolCallDelta
    ArgumentsDelta string

    // EventMessageEnd
    Usage *Usage

    // EventError
    Error error
}

type EventType int
const (
    EventTextDelta      EventType = iota
    EventToolCallStart
    EventToolCallDelta
    EventToolCallEnd
    EventMessageEnd
    EventError
)

type Usage struct {
    InputTokens  int
    OutputTokens int
}
```

### 6.2 Tool 接口

```go
// Tool 是所有可被 LLM 调用的工具的统一接口
type Tool interface {
    Name() string
    Description() string

    // Parameters 返回 JSON Schema 格式的参数定义
    Parameters() map[string]any

    // Execute 执行工具，返回结果
    Execute(ctx context.Context, params json.RawMessage) (ToolResult, error)

    // IsReadOnly 标记是否为只读操作（影响并行执行策略）
    IsReadOnly() bool

    // PermissionLevel 返回该工具所需的权限级别
    PermissionLevel() PermissionLevel
}

// ToolResult 工具执行结果
type ToolResult struct {
    Content   string
    IsError   bool
    Truncated bool            // 内容是否被截断
    Metadata  map[string]any
}

// ToolCall 代表 LLM 请求执行的一个工具调用
type ToolCall struct {
    ID     string
    Name   string
    Params json.RawMessage
}
```

### 6.3 PermissionPolicy 接口

```go
type PermissionPolicy interface {
    // Check 返回对该工具调用的权限决策
    Check(ctx context.Context, call ToolCall) PermissionDecision
}

type PermissionDecision int
const (
    PermissionAllowed          PermissionDecision = iota
    PermissionDenied
    PermissionNeedConfirmation  // 需要向用户询问
)

type PermissionLevel int
const (
    PermissionRead       PermissionLevel = iota // 只读：ReadFile, Search
    PermissionWrite                              // 写入：EditFile, WriteFile
    PermissionExecute                            // 执行：BashExec（安全命令）
    PermissionDangerous                          // 危险：BashExec（破坏性命令）、Git Push
)
```

---

## 7. Agent 主循环设计

### 7.1 完整的 Agentic Loop（修正版）

原始提示词描述的 6 步循环缺失了多个关键环节。完整设计如下：

```
┌─────────────────────────────────────────────┐
│                Agent Loop                    │
│                                              │
│  1. 读取用户输入                              │
│     └─ 处理 /slash 内置命令                   │
│                                              │
│  2. 加载项目上下文                            │
│     └─ CLAUDE.md / .claude-agent/MEMORY.md  │
│                                              │
│  3. 检查 Context Window                      │
│     ├─ 计算当前 token 总量                    │
│     └─ 如接近上限（>80%）→ 触发自动压缩        │
│                                              │
│  4. 调用 LLM（Streaming）                    │
│     ├─ 文本 delta → 实时渲染到 TUI            │
│     └─ tool_call → 收集完整参数后处理         │
│                                              │
│  5. 有 Tool Call？                           │
│     ├─ 是 → 显示计划 + 参数                  │
│     │      → 权限检查（ask/allow/deny）       │
│     │      → 执行工具（读操作并行，写操作串行）  │
│     │      → 将结果追加到消息历史              │
│     │      → 回到步骤 3                      │
│     └─ 否 → 输出 Final Answer，等待下一轮输入  │
│                                              │
│  循环上限：max_iterations = 25（防无限循环）   │
└─────────────────────────────────────────────┘
```

### 7.2 System Prompt 模板

```
You are aictl, an AI coding assistant running in the terminal.
You help users with software engineering tasks by reading and modifying files,
running commands, and searching codebases.

## Capabilities
You have access to the following tools:
{tool_descriptions}

## Rules
- Before making changes, read the relevant files to understand the current code
- Prefer small, targeted edits over rewriting entire files
- Always explain what you're about to do before calling tools
- If a command might be destructive, warn the user explicitly
- Do not make assumptions about file contents — read them first

## Project Context
{project_context}

## Working Directory
{cwd}
```

### 7.3 Streaming + Tool Call 状态机

```
状态：
  Idle            -- 等待响应开始
  StreamingText   -- 接收文本增量
  StreamingTool   -- 接收工具调用参数（累积 JSON buffer）
  ToolsPending    -- 所有 block 接收完毕，有待执行的工具
  ExecutingTools  -- 工具正在执行
  Done            -- 本轮完成

关键细节：
- StreamingTool 状态下：为每个 tool_call 维护独立的 JSON buffer
- ToolsPending → ExecutingTools：批量提交，按只读/写入分组并行/串行执行
- ExecutingTools → Idle：将所有工具结果作为一条 assistant 消息追加，重启循环
```

### 7.4 Context Window 管理策略

```
Token 预算分配：
  总 context window（如 200K）
  ├─ System prompt + project context：15%（30K）
  ├─ 当前用户输入 + 工具结果：20%（40K）
  └─ 历史消息：65%（130K）

当历史消息超出 65% 预算时触发压缩：
  1. 保留最近 5 轮完整对话（不压缩）
  2. 将更早的消息批量发给 LLM，请求生成摘要
  3. 用摘要替换原始消息，标记为 [Summarized]
  4. 工具返回结果（尤其大文件内容）是首要压缩目标

Token 计数方案：
  - 不在 Go 端精确复刻各家 tokenizer
  - 使用字符数 / 4 作为粗略估算
  - 用 API 返回的 usage.input_tokens 校准实际消耗
  - 每轮结束后更新 session 的累计 token 统计
```

---

## 8. 工具系统设计

### 8.1 核心工具清单

| 工具名 | 参数 | 只读 | 权限级别 | 说明 |
|--------|------|------|---------|------|
| `read_file` | `path, offset?, limit?` | ✅ | Read | 读取文件，支持分页 |
| `edit_file` | `path, old_string, new_string` | ❌ | Write | Exact string replace |
| `write_file` | `path, content` | ❌ | Write | 新建或覆盖整个文件 |
| `bash` | `command, timeout?` | ❌ | Execute/Dangerous | 执行 shell 命令 |
| `glob` | `pattern, path?` | ✅ | Read | 文件模式匹配 |
| `grep` | `pattern, path?, type?` | ✅ | Read | 内容搜索 |
| `list_dir` | `path` | ✅ | Read | 目录列表 |
| `git_status` | — | ✅ | Read | git status |
| `git_diff` | `ref?` | ✅ | Read | git diff |
| `git_commit` | `message, files?` | ❌ | Write | git add + commit |
| `git_push` | `remote?, branch?` | ❌ | Dangerous | git push |

### 8.2 EditFile 参数设计（关键纠正）

**原始提示词描述** `EditFile(path, instruction or diff)` 存在严重歧义。

**正确设计：Exact String Replace**

```go
// EditFile 工具参数
type EditFileParams struct {
    FilePath  string `json:"file_path"`
    OldString string `json:"old_string"` // 必须精确匹配文件中的内容
    NewString string `json:"new_string"` // 替换后的内容
}

// 错误处理：
// 1. old_string 不存在 → 报错："The specified text was not found in the file"
// 2. old_string 出现多次 → 报错："Found N occurrences. Provide more context to make it unique"
// 3. 空 old_string + 非空 new_string → 等同于 write_file（追加到文件末尾，或报错）
```

**为什么不用 diff/patch**：LLM 生成标准 diff 格式极不可靠（行号计算错误、上下文行偏移），而 exact string replace 是 LLM 擅长的操作（只需"看到"原文并写出新文本）。这也是 Claude Code 官方的实现方案。

### 8.3 ToolExecutor 设计

```go
type ToolExecutor struct {
    registry       *ToolRegistry
    permission     PermissionPolicy
    defaultTimeout time.Duration  // 默认 30s
    maxOutputBytes int            // 默认 100KB
}

// Execute 执行单个工具调用
// 内部流程：
//   1. 权限检查（Check → ask user if needed）
//   2. 创建带 timeout 的 ctx
//   3. 执行 tool.Execute(ctx, params)
//   4. 超出 maxOutputBytes 时截断，设置 Truncated=true
//   5. 不做自动重试（错误直接返回给 LLM，让 LLM 决策）

// 多工具调用的并发策略：
//   - 所有工具标记 IsReadOnly() == true → errgroup 并发执行
//   - 有任何写操作 → 串行执行（避免竞态）
```

### 8.4 大输出截断策略

| 工具 | 截断策略 |
|------|---------|
| `read_file` | 默认读取前 2000 行；超出时末尾追加 `[Truncated: N total lines. Use offset/limit to read more.]` |
| `bash` | stdout+stderr 合计超过 100KB 时截断 |
| `grep` | 最多返回 50 条匹配结果 |
| `git_diff` | 超过 200 行时截断，提示用 `git_diff --stat` |

---

## 9. Provider 抽象层设计

### 9.1 各 Provider Tool Calling 格式差异

| Provider | Tool Call 位置 | Streaming | 参数格式 |
|----------|--------------|-----------|---------|
| Anthropic | `content[].type="tool_use"` | `content_block_start` + `input_json_delta` | 累积 JSON |
| OpenAI | `choices[].message.tool_calls[]` | `delta.tool_calls[].function.arguments`（增量字符串） | JSON string 拼接 |
| Gemini | `candidates[].content.parts[].functionCall` | 类似 parts 结构 | 直接 object |
| Groq | 同 OpenAI 格式 | 同 OpenAI（有已知 bug） | 同 OpenAI |
| Ollama | `message.tool_calls[]` | **streaming tool call 支持不成熟** | `{function:{name,arguments}}` |
| 国产模型（大多数） | 同 OpenAI 格式 | 同 OpenAI | 同 OpenAI |

### 9.2 中国模型支持

#### 9.2.1 按 API 兼容性分类

**A. OpenAI 兼容（现有 OpenAI adapter 直接覆盖，仅需配置）**

绝大多数主流国产模型已提供 OpenAI 兼容 API，**无需编写新 adapter**，只需在配置中指定不同的 `base_url`：

| 模型 | 厂商 | Base URL | 推荐模型 |
|------|------|----------|---------|
| DeepSeek | 深度求索 | `https://api.deepseek.com` | `deepseek-chat`, `deepseek-reasoner` |
| 通义千问 (Qwen) | 阿里云 | `https://dashscope.aliyuncs.com/compatible-mode/v1` | `qwen-max`, `qwen-plus` |
| GLM / ChatGLM | 智谱 AI | `https://open.bigmodel.cn/api/paas/v4/` | `glm-4`, `glm-4-flash` |
| Kimi | 月之暗面 | `https://api.moonshot.cn/v1` | `moonshot-v1-128k` |
| MiniMax | MiniMax | `https://api.minimax.chat/v1` | `abab6.5-chat` |
| 阶跃星辰 | Stepfun | `https://api.stepfun.com/v1` | `step-1`, `step-2` |
| Yi / 零一万物 | 01.AI | `https://api.lingyiwanwu.com/v1` | `yi-large` |
| 豆包 | 字节跳动 | `https://ark.cn-beijing.volces.com/api/v3` | `doubao-pro-32k` |
| 百川 | Baichuan AI | `https://api.baichuan-ai.com/v1` | `Baichuan4` |

**B. 需要自定义 Adapter（API 格式不兼容，v1.0 暂缓）**

| 模型 | 厂商 | 原因 |
|------|------|------|
| 文心一言 (ERNIE) | 百度 | 自有 OAuth 机制 + 完全不同的 API 格式 |
| 星火 (Spark) | 讯飞 | WebSocket API，不支持 HTTP SSE |

> 文心/星火的 tool calling 能力较弱，v1.0 暂不支持，后续可按需添加独立 adapter。

#### 9.2.2 DeepSeek 的特殊说明

DeepSeek 是目前国产模型中 **tool calling + coding 能力最强** 的，建议作为国产模型的首选默认：
- `deepseek-chat`（DeepSeek-V3）：通用对话 + 代码，性价比最高
- `deepseek-reasoner`（DeepSeek-R1）：推理型，适合复杂编程任务，但 **不支持 tool calling**，agent 模式下需使用 `deepseek-chat`

#### 9.2.3 国产模型 Tool Calling 能力对比

| 模型 | Tool Calling | Streaming Tool Call | 推荐用于 Agent |
|------|-------------|---------------------|--------------|
| DeepSeek-V3 | ✅ | ✅ | ✅ 首选 |
| Qwen-Max | ✅ | ✅ | ✅ |
| GLM-4 | ✅ | ✅ | ✅ |
| Kimi (Moonshot) | ✅ | ✅ | ✅ |
| DeepSeek-R1 | ❌ | — | ❌（推理模式不支持） |
| Yi-Large | ✅ | 部分 | ⚠️ 需测试 |
| 豆包 | ✅ | ✅ | ✅ |
| MiniMax | ⚠️ 部分 | ⚠️ | ⚠️ 需测试 |

### 9.3 Adapter 职责

每个 adapter 负责：
1. 将统一 `ChatRequest` 转换为该 provider 的 API 请求格式
2. 将该 provider 的 streaming 响应转换为统一 `StreamEvent` 序列
3. 处理该 provider 特有的错误码（如 Anthropic 的 529 overloaded → 指数退避重试）
4. 将工具 schema 从统一格式转换为该 provider 要求的格式

**特殊处理**：
- **Ollama**：强制使用 non-streaming 模式获取 tool calls（文本部分仍可 streaming）
- **Groq**：有 streaming tool call 的已知 bug，需要额外容错处理
- **DeepSeek-R1**：检测到 `deepseek-reasoner` 模型时，自动禁用 tool calling，降级为纯对话模式

### 9.4 Provider 配置与注册

```yaml
# ~/.config/aictl/config.yaml

provider: anthropic          # 当前使用的 provider
model: claude-opus-4-5       # 当前使用的模型

providers:
  # === 国际模型 ===
  anthropic:
    api_key: ${ANTHROPIC_API_KEY}
    model: claude-opus-4-5

  openai:
    api_key: ${OPENAI_API_KEY}
    model: gpt-4o
    base_url: https://api.openai.com/v1

  gemini:
    api_key: ${GOOGLE_API_KEY}
    model: gemini-2.0-flash-exp

  groq:
    api_key: ${GROQ_API_KEY}
    model: llama-3.3-70b-versatile
    base_url: https://api.groq.com/openai/v1

  ollama:
    base_url: http://localhost:11434
    model: llama3.2

  # === 国产模型（OpenAI 兼容，复用 openai adapter）===
  deepseek:
    api_key: ${DEEPSEEK_API_KEY}
    model: deepseek-chat
    base_url: https://api.deepseek.com

  qwen:
    api_key: ${DASHSCOPE_API_KEY}
    model: qwen-max
    base_url: https://dashscope.aliyuncs.com/compatible-mode/v1

  glm:
    api_key: ${ZHIPU_API_KEY}
    model: glm-4
    base_url: https://open.bigmodel.cn/api/paas/v4/

  kimi:
    api_key: ${MOONSHOT_API_KEY}
    model: moonshot-v1-128k
    base_url: https://api.moonshot.cn/v1

  doubao:
    api_key: ${ARK_API_KEY}
    model: doubao-pro-32k
    base_url: https://ark.cn-beijing.volces.com/api/v3

  stepfun:
    api_key: ${STEPFUN_API_KEY}
    model: step-1
    base_url: https://api.stepfun.com/v1
```

> **架构说明**：所有国产 OpenAI 兼容模型共用同一个 `openai` adapter 实例，通过 `base_url` 和 `api_key` 区分。Provider 注册表在初始化时根据配置自动创建对应的 adapter 实例，无需为每个国产模型单独编写代码。

---

## 10. 权限系统设计

### 10.1 操作分级

```
Level 0 - Read（自动允许）
  read_file, glob, grep, list_dir, git_status, git_diff

Level 1 - Write（默认询问）
  edit_file, write_file, git_commit

Level 2 - Execute（默认询问，显示具体命令）
  bash（安全命令白名单内，如 go test, npm install）

Level 3 - Dangerous（醒目警告，强制确认）
  bash（破坏性命令：rm, curl|sh 等）
  git_push
```

### 10.2 三层白名单配置

```yaml
# ~/.config/aictl/config.yaml

permissions:
  mode: interactive          # interactive | auto-approve | yolo

  # 层 1：工具级别自动批准
  auto_approve_tools:
    - read_file
    - glob
    - grep
    - list_dir
    - git_status
    - git_diff

  # 层 2：命令级别（针对 bash 工具）
  # 支持前缀匹配，列表内的命令自动批准
  allowed_commands:
    - "go test"
    - "go build"
    - "npm test"
    - "make"
    - "ls"
    - "cat"

  # 层 3：路径级别（针对 edit_file/write_file）
  # 限制可修改的文件路径范围
  allowed_paths:
    - "./src/**"
    - "./tests/**"

  # 命令黑名单（即使在 auto-approve 或 yolo 模式下也强制拒绝）
  denied_commands:
    - "rm -rf /"
    - "curl * | sh"
    - "sudo"
```

### 10.3 运行模式

| 模式 | Flag | 说明 |
|------|------|------|
| Interactive | 默认 | Level 2+ 操作询问用户确认 |
| Auto-approve | `--auto-approve` | 根据白名单配置自动批准 |
| YOLO | `--yolo` | 所有操作自动批准（启动时显示醒目警告） |

---

## 11. 会话与上下文管理

### 11.1 会话存储

```
~/.local/share/aictl/sessions/
  └── {session-id}.db     # SQLite 数据库

会话数据库结构：
  - messages 表：role, content_json, token_count, created_at
  - tool_calls 表：message_id, tool_name, params_json, result_json
  - metadata 表：session_id, project_path, provider, model, total_tokens, created_at
```

### 11.2 项目上下文文件

扫描顺序（优先级从高到低）：
1. `{cwd}/CLAUDE.md`（项目级别，遵循原始 Claude Code 约定）
2. `{git-root}/CLAUDE.md`（如果 cwd 不是 git root）
3. `{cwd}/.claude-agent/MEMORY.md`（项目本地记忆）
4. `~/.config/aictl/CLAUDE.md`（用户全局上下文）

大小限制：单文件 8KB，总计 16KB。超出截断并标记 `[Truncated]`。

### 11.3 内置 Slash 命令

| 命令 | 说明 |
|------|------|
| `/clear` | 清空当前会话消息历史 |
| `/compact` | 手动触发上下文压缩 |
| `/model <name>` | 切换当前使用的模型 |
| `/provider <name>` | 切换 LLM provider |
| `/config` | 查看/修改配置 |
| `/help` | 显示帮助信息 |
| `/cost` | 显示当前会话的 token 用量和估算费用 |
| `/save` | 保存当前会话到文件 |
| `/sessions` | 列出历史会话 |
| `/resume <id>` | 恢复历史会话 |
| `/done` `/exit` | 退出 |

---

## 12. TUI 层设计

### 12.1 bubbletea 架构

```
App (bubbletea Model)
├── ChatView              -- 主对话视图（消息历史 + 流式输出）
│   ├── MessageList       -- 已完成的消息列表
│   └── StreamingMessage  -- 正在流式输出的当前消息
├── InputArea             -- 多行输入框（bubbles/textarea）
├── ConfirmDialog         -- 工具执行权限确认弹窗
├── Spinner               -- LLM 响应等待状态
└── StatusBar             -- 底部状态栏（provider/model/tokens/cost）
```

### 12.2 消息渲染规则

| 角色 | 渲染方式 | 样式 |
|------|---------|------|
| User | 原始文本 | 右对齐，青色前缀 |
| Assistant | glamour Markdown | 左对齐，白色 |
| Tool Call | 代码块（工具名 + 参数 JSON） | 黄色边框 |
| Tool Result | 代码块（截断显示）| 绿色（成功）/ 红色（失败）|
| System | 斜体灰色 | 居中 |

### 12.3 流式输出处理

```
LLM streaming → goroutine 发送 bubbletea Msg → Model.Update() 追加文本
                                               → View() 重新渲染当前 block

关键：不要在 Update() 中做 I/O，所有 LLM 通信在独立 goroutine 中完成
```

---

## 13. 配置系统设计

### 13.1 配置文件位置

```
优先级从高到低：
  1. --config flag 指定路径
  2. $AICTL_CONFIG 环境变量
  3. ~/.config/aictl/config.yaml（XDG 标准路径）
  4. ~/.aictl/config.yaml（兼容路径）
```

### 13.2 环境变量覆盖规则

```bash
ANTHROPIC_API_KEY=xxx    # 自动覆盖 providers.anthropic.api_key
OPENAI_API_KEY=xxx       # 自动覆盖 providers.openai.api_key
GOOGLE_API_KEY=xxx       # 自动覆盖 providers.gemini.api_key
AICTL_PROVIDER=openai    # 覆盖 provider 选择
AICTL_MODEL=gpt-4o       # 覆盖 model 选择
AICTL_AUTO_APPROVE=true  # 启用 auto-approve 模式
```

### 13.3 aictl init 命令

首次运行 `aictl init` 引导用户：
1. 选择默认 provider
2. 输入 API key（存储到配置文件，建议设置文件权限 0600）
3. 选择默认模型
4. 选择权限模式
5. 生成 `~/.config/aictl/config.yaml`

---

## 14. 开源规范

### 14.1 开源协议

**选择：Apache License 2.0**

理由：
- **专利保护**：包含显式专利授权条款，防止专利钓鱼
- **企业友好**：大型企业更信赖 Apache 2.0（Kubernetes、Docker、Terraform 均使用）
- **Go 生态主流**：与 Go 重量级项目保持一致
- **商标条款**：第 6 条明确区分商标使用，保护项目品牌

### 14.2 必备文件

| 文件 | 必要性 | 内容要点 |
|------|--------|---------|
| `README.md` | 必需 | Features / Quick Start / Installation / Usage / Configuration / Contributing / License |
| `LICENSE` | 必需 | Apache 2.0 全文 |
| `CONTRIBUTING.md` | 必需 | 开发环境设置、代码风格（gofmt/golangci-lint）、PR 流程、Commit 规范 |
| `SECURITY.md` | 必需 | 漏洞报告邮箱、响应时间承诺、负责任披露政策 |
| `CODE_OF_CONDUCT.md` | 推荐 | Contributor Covenant v2.1 |
| `CHANGELOG.md` | 推荐 | Keep a Changelog 格式，初期手动维护 |
| `.goreleaser.yaml` | 发布时必需 | 多平台构建、Homebrew tap、校验和 |
| `.golangci.yml` | 推荐 | 启用 errcheck/govet/staticcheck/unused/ineffassign |

### 14.3 README 必须包含的章节

```markdown
# aictl

AI coding agent in your terminal. Open-source, model-agnostic.

## Features
## Quick Start
## Installation
  - Homebrew: brew install your-org/tap/aictl
  - Go: go install github.com/your-org/aictl@latest
  - Binary: GitHub Releases
## Configuration
  - API Keys
  - Provider Selection
  - Permission Modes
## Usage
  - Interactive mode
  - Non-interactive mode: aictl run -p "refactor this function"
  - Slash commands
## Supported Providers
## Architecture
## Contributing
## License
```

---

## 15. CI/CD 与发布策略

### 15.1 GitHub Actions CI

```yaml
# .github/workflows/ci.yml
name: CI
on:
  push:
    branches: [main]
  pull_request:
    branches: [main]

jobs:
  lint:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with: { go-version: '1.23' }
      - uses: golangci/golangci-lint-action@v6

  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with: { go-version: '1.23' }
      - run: go test -race -coverprofile=coverage.out ./...

  build:
    strategy:
      matrix:
        os: [ubuntu-latest, macos-latest, windows-latest]
        arch: [amd64, arm64]
    runs-on: ${{ matrix.os }}
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with: { go-version: '1.23' }
      - run: GOARCH=${{ matrix.arch }} go build -o aictl .
```

### 15.2 GoReleaser 配置要点

```yaml
# .goreleaser.yaml
version: 2
builds:
  - env: [CGO_ENABLED=0]
    goos: [linux, darwin, windows]
    goarch: [amd64, arm64]
    ldflags:
      - -s -w
      - -X main.version={{.Version}}
      - -X main.commit={{.Commit}}
      - -X main.date={{.Date}}

archives:
  - format_overrides:
      - goos: windows
        format: zip

checksum:
  name_template: 'checksums.txt'

brews:
  - repository:
      owner: your-org
      name: homebrew-tap
    homepage: https://aictl.dev
    description: "AI coding agent in your terminal"

changelog:
  sort: asc
  filters:
    exclude: ['^docs:', '^test:', '^chore:']
```

---

## 16. 开发路线图

### Phase 1: MVP（目标：能跑起来）

**目标**：最小可用版本，打通核心流程

- [ ] 项目骨架（go.mod, cobra, 目录结构）
- [ ] Config 系统（yaml.v3 加载，环境变量覆盖）
- [ ] Anthropic Provider adapter（streaming + tool use）
- [ ] 核心工具：`read_file`, `write_file`, `bash`, `glob`
- [ ] Agent Loop（基础版，无 token 管理）
- [ ] 简单 REPL（无 TUI，bufio.Scanner 输入 + fmt.Println 输出）
- [ ] 基础权限询问（终端 y/n 确认）

**里程碑**：`v0.1.0` — 能在终端和 Anthropic Claude 对话、执行工具

### Phase 2: 核心工具完善

- [ ] `edit_file`（exact string replace，含错误处理）
- [ ] `grep`（ripgrep 风格搜索）
- [ ] `list_dir`
- [ ] Git 工具（exec git wrapper）
- [ ] 权限系统完整实现（三层白名单）
- [ ] `--auto-approve` flag
- [ ] CLAUDE.md 加载与注入

**里程碑**：`v0.2.0` — 工具链完整，可实际用于代码任务

### Phase 3: TUI 与体验

- [ ] bubbletea TUI 框架搭建
- [ ] 流式输出渲染
- [ ] glamour Markdown 渲染
- [ ] 多行输入（bubbles/textarea）
- [ ] Spinner 状态指示
- [ ] 权限确认弹窗
- [ ] 状态栏（provider/model/tokens）

**里程碑**：`v0.3.0` — 专业 CLI 交互体验

### Phase 4: 会话管理

- [ ] SQLite 会话存储
- [ ] 消息历史持久化
- [ ] Context Window 管理（token 计数 + 自动压缩）
- [ ] Slash 命令完整实现（/clear /compact /sessions /resume 等）
- [ ] 成本显示（/cost）

**里程碑**：`v0.4.0` — 完整的会话管理

### Phase 5: 多 Provider

- [ ] OpenAI adapter（覆盖 OpenAI/Groq/Ollama）
- [ ] Gemini adapter
- [ ] Provider 热切换（/provider 命令）
- [ ] 模型列表展示

**里程碑**：`v0.5.0` — 完整多 Provider 支持

### Phase 6: 生态与发布

- [ ] MCP（Model Context Protocol）工具扩展支持
- [ ] Homebrew tap 配置
- [ ] GoReleaser 发布流程
- [ ] 完整文档（docs/）
- [ ] 安装脚本
- [ ] 用户引导（aictl init）

**里程碑**：`v1.0.0` — 生产可用，公开发布

---

## 17. 参考项目

| 项目 | 语言 | 说明 |
|------|------|------|
| **[OpenCode](https://github.com/opencode-ai/opencode)** | Go | **最相关**。Go 实现的 AI coding agent CLI，技术选型几乎与本设计一致（Cobra + bubbletea + 官方 SDK thin adapter），是最重要的参考 |
| [aider](https://github.com/paul-gauthier/aider) | Python | 成熟的 AI coding CLI，架构设计参考 |
| [Claude Code CLI](https://claude.ai/claude-code) | TypeScript | 行为目标参考 |
| [Eino](https://github.com/cloudwego/eino) | Go | ByteDance 的 LLM 框架，过重但接口设计可参考 |
| [mark3labs/mcp-go](https://github.com/mark3labs/mcp-go) | Go | MCP 协议的 Go 实现，工具扩展必用 |

---

*文档版本：v0.1 | 生成日期：2026-02-19 | 基于三个专业 agent 的并行深度分析综合产出*

# aictl 项目总结

> 用 Go 语言从零构建一个 AI 编程助手 CLI 工具

---

## 项目简介

`aictl` 是一个开源的 AI 编程助手命令行工具，使用 **Go 1.24** 编写。它可以连接多种大语言模型（Anthropic Claude、DeepSeek、OpenAI 等），在终端中提供类似 Claude Code 的交互式编程辅助体验。

核心特性：
- 🤖 **多 Provider 支持** — Anthropic 原生 API + 所有 OpenAI 兼容接口
- 🛠️ **工具调用** — 文件读写、Shell 命令、代码搜索、Git 操作等 11 个内置工具
- 🔒 **权限管理** — 四级权限 + 交互式确认，保证操作安全
- 💬 **持久化会话** — SQLite 存储对话历史，支持跨会话恢复
- 🖥️ **全屏 TUI** — 基于 Bubbletea 的终端 UI，支持流式输出、Markdown 渲染、工具进度展示
- ⚡ **上下文压缩** — 超出 Token 限制时自动摘要，无限对话不截断

---

## 项目架构

```
aictl/
├── main.go                        # 程序入口
├── cmd/
│   ├── root.go                    # Cobra 根命令、全局 Flag、Provider 构建
│   ├── chat.go                    # 交互式对话模式
│   ├── run.go                     # 单次执行模式（非交互）
│   ├── init.go                    # aictl init 初始化配置
│   └── version.go                 # aictl version 版本信息
└── internal/
    ├── agent/
    │   ├── agent.go               # Agent 主结构体、构造函数
    │   ├── loop.go                # 核心 Agentic Loop
    │   ├── context.go             # 系统提示词构建
    │   └── retry.go               # API 调用重试逻辑
    ├── provider/
    │   ├── provider.go            # Provider 接口定义、消息类型
    │   ├── anthropic.go           # Anthropic Claude 实现
    │   ├── openai.go              # OpenAI 兼容实现（DeepSeek/Kimi/Qwen 等）
    │   └── registry.go            # Provider 注册表
    ├── tools/
    │   ├── tool.go                # Tool 接口定义、权限级别
    │   ├── registry.go            # 工具注册表
    │   ├── executor.go            # 工具执行器（权限检查 + 执行）
    │   ├── bash.go                # bash 命令执行
    │   ├── readfile.go            # 文件读取
    │   ├── editfile.go            # 文件编辑（精确字符串替换）
    │   ├── writefile.go           # 文件写入
    │   ├── glob.go                # 文件模式匹配
    │   ├── grep.go                # 内容搜索（ripgrep）
    │   ├── listdir.go             # 目录列表
    │   └── git.go                 # Git 操作（status/diff/commit/push）
    ├── session/
    │   ├── session.go             # Session 结构体、消息管理
    │   ├── store.go               # Store 接口定义
    │   ├── sqlite.go              # SQLite 持久化实现
    │   ├── context.go             # 上下文压缩（CompactHistory/SplitTurns）
    │   ├── budget.go              # Token 预算计算
    │   └── summarize.go           # LLM 摘要压缩
    ├── permission/
    │   ├── permission.go          # 权限询问接口
    │   └── policy.go              # 权限策略（interactive/auto-approve/yolo）
    ├── config/
    │   └── config.go              # YAML 配置加载
    └── tui/
        ├── model.go               # Bubbletea Model（核心 TUI 逻辑）
        ├── run.go                 # TUI 启动入口
        ├── io.go                  # IO 接口定义
        ├── tuiio.go               # TUI IO 实现（线程安全）
        └── plain.go               # Plain IO 实现（非 TUI 模式）
```

---

## 核心模块详解

### 1. Agent Loop（`internal/agent/loop.go`）

Agent 的核心工作逻辑，一个标准的 Tool-Use 循环：

```
用户输入
  → 构建请求（携带工具 Schema）
  → 流式调用 LLM
  → 收集文本 Delta（实时展示）+ 工具调用请求
  → 如果有工具调用：
      → 权限检查 → 用户确认 → 执行工具
      → 把工具结果追加到历史
      → 继续循环
  → 如果没有工具调用：返回，等待下一轮用户输入
```

关键设计：
- **最大迭代次数**：默认 25 次，防止无限循环
- **自动重试**：网络错误、速率限制等可重试错误，指数退避
- **Token 追踪**：每次 API 响应更新 Token 计数
- **自动压缩**：Token 超过阈值时触发 LLM 摘要

### 2. Provider 抽象（`internal/provider/`）

统一的 LLM Provider 接口：

```go
type Provider interface {
    Chat(ctx context.Context, req *ChatRequest) (<-chan StreamEvent, error)
    ContextWindow() int
}
```

流式事件类型：
- `EventTextDelta` — 文本增量（实时渲染）
- `EventToolCallDone` — 完整工具调用（含参数）
- `EventDone` — 完成信号（含 Token 用量）
- `EventError` — 流式错误

支持的 Provider：
| Provider | 类型 | 备注 |
|---------|------|------|
| Anthropic | 原生 SDK | Claude 3.5/4 系列 |
| DeepSeek | OpenAI 兼容 | deepseek-chat, deepseek-reasoner |
| OpenAI | OpenAI 兼容 | GPT-4o 等 |
| Kimi | OpenAI 兼容 | moonshot-v1 系列 |
| Qwen | OpenAI 兼容 | qwen-plus 等 |
| GLM | OpenAI 兼容 | GLM-4 系列 |
| Groq | OpenAI 兼容 | llama, mixtral 等 |
| Minimax | OpenAI 兼容 | - |
| Doubao | OpenAI 兼容 | 火山引擎 |

### 3. 工具系统（`internal/tools/`）

所有工具实现统一接口：

```go
type Tool interface {
    Name() string                                          // snake_case 名称
    Description() string                                   // 给 LLM 的描述
    Parameters() map[string]any                            // JSON Schema
    Execute(ctx context.Context, params json.RawMessage) (ToolResult, error)
    IsReadOnly() bool
    PermissionLevel() PermissionLevel
}
```

内置工具列表：

| 工具 | 权限级别 | 说明 |
|-----|---------|------|
| `read_file` | Read（自动允许）| 读取文件内容，支持行号范围 |
| `write_file` | Write（询问）| 覆盖写入文件 |
| `edit_file` | Write（询问）| 精确字符串替换（替代 sed）|
| `glob` | Read（自动允许）| 文件模式匹配 |
| `grep` | Read（自动允许）| 内容搜索，支持正则 |
| `list_dir` | Read（自动允许）| 目录列表 |
| `bash` | Execute（询问）| Shell 命令执行，有超时限制 |
| `git_status` | Read（自动允许）| Git 工作区状态 |
| `git_diff` | Read（自动允许）| 文件差异对比 |
| `git_commit` | Write（询问）| 提交变更 |
| `git_push` | Dangerous（强制确认）| 推送到远程（需醒目警告）|

### 4. 权限系统（`internal/permission/`）

四级权限控制：

```
PermissionRead      → 只读，自动允许，无需确认
PermissionWrite     → 写入，默认询问用户
PermissionExecute   → 执行，默认询问，展示完整命令
PermissionDangerous → 危险，强制确认，醒目警告
```

三种策略模式：

| 模式 | 触发 | 行为 |
|-----|-----|------|
| `interactive` | 默认 | 所有非只读操作都询问用户 |
| `auto-approve` | `--auto-approve` | 自动批准所有操作 |
| `yolo` | 配置文件 | 同 auto-approve |

### 5. 会话管理（`internal/session/`）

**Session 结构**：
- 消息历史（`[]provider.Message`）
- Token 计数（prompt/completion/total）
- 摘要缓存（上下文压缩后的摘要）

**SQLite 持久化**：
- 使用 `modernc.org/sqlite`（纯 Go，无 CGO）
- 存储路径：`~/.config/aictl/sessions/`
- 每个 session 有唯一 UUID

**上下文压缩策略**：
1. **CompactHistory** — 超出预算时，保留最近 N 轮 + 前缀摘要
2. **SplitTurns** — 将消息按完整对话轮次切分（保证不断开中间的工具调用）
3. **LLM 摘要** — Token 超过阈值时，让模型自动生成对话摘要

**Token 预算计算**：
```
ContextWindow = Provider 上下文窗口（如 200k）
SystemPrompt  = 系统提示词预估 Token
HistoryMax    = ContextWindow × 0.7 - SystemPromptTokens
CompactThreshold = ContextWindow × 0.8
```

---

## TUI 界面（`internal/tui/`）

基于 [Bubbletea](https://github.com/charmbracelet/bubbletea) 构建的全屏终端 UI。

### 架构

遵循 Bubbletea 的 Elm 架构（Model / Update / View）：

```
RunTUI()
  ├── 启动 Bubbletea 程序（Alt-Screen 模式）
  └── goroutine: 运行 agentFn(io)
        └── 通过 TuiIO 发送消息到 TUI
              ├── p.Send(textDeltaMsg{})   → 实时文字流
              ├── p.Send(toolStartMsg{})   → 工具开始
              ├── p.Send(toolDoneMsg{})    → 工具完成
              ├── p.Send(tokenUpdateMsg{}) → Token 更新
              └── p.Send(agentDoneMsg{})   → Agent 完成
```

### 欢迎页

```
╭─ aictl ──────────────────────────────────╮
│                                           │
│  █▀▀▀▀▀█   Provider: deepseek            │
│  █ ●  ● █   Model:    deepseek-chat       │
│  █  ▲  █   Session:  a1b2c3d4            │
│  █ ▄▄▄ █                                 │
│   ▀▀▀▀▀                                  │
│                                           │
╰──────────────────────────────────────────╯
```

### 布局

```
┌─────────────────────────────┐
│ 对话内容（Viewport 或直接渲染）  │
│ ...                          │
│ ❯ 用户输入框                  │  ← 内容结束后紧跟输入框
│                              │
│  （空白填充）                 │  ← 剩余空间填白
│──────────────────────────────│
│ deepseek-chat │ tokens: 1234 │  ← 底部状态栏（固定）
└─────────────────────────────┘
```

### 关键特性

| 特性 | 说明 |
|-----|------|
| 流式渲染 | 文字增量实时输出，有旋转 Spinner 指示思考中 |
| Markdown | 使用 Glamour 渲染 AI 回复中的代码块、列表等 |
| 工具块 | 工具调用以彩色块展示：工具名 + 参数 + 结果 |
| 确认弹框 | 需要确认的操作弹出内联确认框（y/n/q）|
| 键盘滚动 | PageUp/Down、Shift+↑↓、Home/End 翻阅历史 |
| F2 鼠标 | 按 F2 切换鼠标捕获（开启滚动 vs 保留文字选中）|
| 状态栏 | 绿色 Model 名 + Token 用量 + 当前工具 |
| 噪音过滤 | 过滤 OSC/SGR 转义序列（防止终端查询污染输入框）|

### 已解决的技术难点

**1. `strings.Builder` 值拷贝 panic**

Bubbletea 的 `Update()` 以值传递 Model，Go 的 `strings.Builder` 检测到拷贝会 panic。
解决：将 `content strings.Builder` 改为 `content *strings.Builder`。

**2. OSC 转义序列污染输入框**

终端在 Alt-Screen 模式下发送 `]11;rgb:0000/0000/0000\` 查询背景色，Bubbletea 将其作为 `KeyMsg` 传入，出现在输入框中。
解决：在 `KeyMsg` 处理器中检测并过滤 OSC 序列和 SGR 鼠标序列。

**3. 鼠标滚动 vs 文字选中**

`WithMouseCellMotion()` 开启鼠标捕获后，系统级文字选中功能失效。
解决：默认不开启鼠标捕获（保留文字选中），F2 键运行时切换鼠标模式。

**4. 布局空白问题**

Viewport 固定高度导致内容较少时中间出现大块空白。
解决：动态计算内容高度（用 `lipgloss.Height()` 正确处理 ANSI 序列），内容短时不用 Viewport，直接渲染 + 末尾填充空白。

---

## 配置

配置文件路径：`~/.config/aictl/config.yaml`

```yaml
# 默认使用的 Provider
provider: deepseek

# 默认模型（可被 --model 覆盖）
model: deepseek-chat

# Provider 配置
providers:
  anthropic:
    api_key: sk-ant-xxx
  deepseek:
    api_key: sk-xxx
    base_url: https://api.deepseek.com  # 可省略，内置默认值
  openai:
    api_key: sk-xxx
  kimi:
    api_key: sk-xxx
  qwen:
    api_key: sk-xxx

# 权限模式
permissions:
  mode: interactive   # interactive | auto-approve | yolo
```

也可通过环境变量配置（优先级低于配置文件）：

```bash
export LLM_API_KEY=sk-xxx
export LLM_BASE_URL=https://api.deepseek.com
export LLM_MODEL=deepseek-chat
```

快速初始化：

```bash
aictl init   # 交互式引导配置
```

---

## 使用方式

### 安装

```bash
git clone https://github.com/aictl/aictl.git
cd aictl/aictl
go build -o aictl .
sudo mv aictl /usr/local/bin/
```

### 基本命令

```bash
# 交互式对话（自动检测终端，启用 TUI）
aictl

# 单次执行（非交互，适合脚本）
aictl run "帮我找出所有超过 100 行的 Go 文件"

# 指定 Provider 和 Model
aictl --provider anthropic --model claude-opus-4-6

# 强制关闭 TUI
aictl --tui=false

# 跳过所有工具确认（谨慎使用）
aictl --auto-approve

# 查看版本
aictl version
```

---

## 构建与测试

```bash
# 进入项目目录
cd aictl

# 构建
go build ./...

# 运行所有测试
go test ./...

# 运行指定包测试
go test ./internal/session/... -v

# 带竞态检测
go test -race ./...
```

---

## 依赖库

| 库 | 版本 | 用途 |
|---|-----|------|
| `anthropics/anthropic-sdk-go` | v1.25.0 | Anthropic Claude 官方 SDK |
| `openai/openai-go` | v1.12.0 | OpenAI 兼容接口 |
| `spf13/cobra` | v1.8.1 | CLI 框架 |
| `charmbracelet/bubbletea` | v1.3.10 | TUI 框架（Elm 架构）|
| `charmbracelet/lipgloss` | v1.1.0 | 终端样式（颜色、边框）|
| `charmbracelet/glamour` | v0.9.1 | Markdown 终端渲染 |
| `charmbracelet/bubbles` | v1.0.0 | TUI 组件（textinput、viewport）|
| `modernc.org/sqlite` | v1.46.1 | SQLite（纯 Go，无 CGO）|
| `google/uuid` | v1.6.0 | Session UUID 生成 |
| `gopkg.in/yaml.v3` | v3.0.1 | YAML 配置解析 |
| `golang.org/x/term` | v0.32.0 | 终端检测（isatty）|

---

## 开发历程回顾

这个项目从零开始，跨越多个开发阶段：

### 阶段一：骨架搭建
- Cobra CLI 框架、配置加载、Provider 接口抽象
- Anthropic 和 OpenAI 兼容双实现
- 基础 Tool 接口 + 11 个内置工具

### 阶段二：Agent 核心
- Agentic Loop（流式 → 工具调用 → 循环）
- 权限系统（四级 + 三种策略）
- Plain IO 实现（非 TUI 模式可用）

### 阶段三：会话持久化
- SQLite 会话存储
- 上下文压缩算法（CompactHistory + SplitTurns）
- Token 预算计算
- LLM 摘要自动压缩

### 阶段四：TUI 界面
- Bubbletea 全屏 TUI 基础框架
- 流式渲染、Spinner、工具块样式、Markdown
- 内联确认弹框

### 阶段五：TUI 优化（品牌化）
- 欢迎页（像素猫 Logo + Provider/Model/Session 信息）
- 自动检测终端，默认开启 TUI
- 状态栏：Model 名（绿色）+ Token + 工具状态
- 输入提示符从 `>` 升级为 `❯`
- 解决多个 TUI Bug：Builder 拷贝 panic、OSC 序列污染、布局空白、鼠标冲突

---

## 项目亮点

1. **纯 Go 实现** — 无 Python 依赖，单二进制分发，编译极快
2. **零 CGO** — SQLite 使用纯 Go 实现（modernc.org/sqlite），跨平台编译无障碍
3. **Provider 可扩展** — 新增 Provider 只需实现 `Chat()` 接口，5 行配置搞定
4. **工具安全** — 四级权限 + 交互确认，不会悄悄执行危险操作
5. **上下文无限** — 自动摘要压缩，不会因为 Token 超限而崩溃
6. **TUI 体验** — 参考 Claude Code 设计，流式输出、工具可视化、品牌欢迎页

---

*该项目为学习性项目，展示如何用 Go 语言完整实现一个 AI 编程助手，涵盖 LLM 集成、工具调用、TUI 开发、会话管理等核心技术栈。*

# aictl 改动记录 2026-02-20

## 一、TUI 工具调用输出折叠（减少垂直空间占用）

**文件：** `aictl/internal/tui/model.go`

### 问题

LLM 连续调用多个工具时（如 7 次 web_fetch），每个完成的工具调用显示 3 行（工具名 + JSON 参数 + 结果摘要），整个屏幕被工具输出塞满，用户很难看到 LLM 的文字回复。

### 改动

#### 1. `renderToolDone()` — 完成的工具折叠为单行

改前（3 行）：
```
│ web_fetch
│ {"url": "https://...", "prompt": "..."}
│ ✓ URL: https://... Prompt: 获取这个...
```

改后（1 行）：
```
│ web_fetch  ✓ URL: https://... Prompt: 获取这个...
```

- 去掉了 JSON 参数行（冗余，结果已包含关键信息）
- 成功：`│ tool_name  ✓ result_summary`
- 失败：`│ tool_name  ✗ error_summary`

#### 2. `renderToolRunning()` — 运行中从 3 行压缩到 2 行

改前（3 行）：
```
│ web_fetch
│ {"url": "https://...", "prompt": "..."}
│ ⠋ running... (3s)  esc to cancel
```

改后（2 行）：
```
│ web_fetch  {"url": "https://...", "prompt": "..."}
│ ⠋ running... (3s)  esc to cancel
```

工具名和参数放同一行。

#### 3. `renderConfirmBlock()` 保持不变

需要用户审查的操作不做压缩。

### 效果

7 次工具调用完成后：改前 21+ 行 → 改后 7 行，节省 66% 垂直空间。

---

## 二、WebFetch 缓存 + 内容截断

**文件：** `aictl/internal/tools/web_fetch.go`

### 问题

1. 同一 URL 重复抓取浪费网络和时间
2. 超长页面 markdown 全部塞给 LLM，浪费大量 token

### 改动

#### 1. 15 分钟内存缓存

```go
var fetchCache = map[string]fetchCacheEntry{}
const fetchCacheTTL = 15 * time.Minute
```

- 同一 URL 在 15 分钟内重复调用直接返回缓存
- 缓存命中时结果标注 `(cached)`
- 缓存超过 100 条时自动清理过期项
- 并发安全（`sync.Mutex`）

#### 2. 2000 行截断

```go
const fetchMaxLines = 2000
```

- 转为 markdown 后，超过 2000 行的内容自动截断
- 截断时末尾标注 `[Content truncated to first 2000 lines]`
- 防止超长页面撑爆 LLM 上下文窗口

### 关键参数

| 参数 | 值 | 说明 |
|---|---|---|
| `fetchCacheTTL` | 15 分钟 | 缓存过期时间 |
| `fetchMaxLines` | 2000 行 | 内容行数上限 |
| `fetchMaxBodySize` | 5MB | HTTP body 大小上限（原有） |

---

## 三、未来待做（已调研，暂不实现）

### 小模型提取（方案 3）

**调研结论：** Claude Code 的 WebFetch 是 Anthropic API 的 server_tool_use（服务端内建工具），抓取和信息提取都在服务端完成。aictl 无法复制这个架构，但可以：

- 抓取后调用一次便宜模型（如 DeepSeek）根据 `prompt` 提取信息
- 只把摘要返回给主 LLM，大幅减少 token 消耗

**前置条件：**
- 需要在 aictl 内支持"辅助模型"调用（当前只有主模型）
- 考虑额外 API 延迟和成本

### VPS 代理抓取

**场景：** 国内网络环境下部分 URL 不可达（GitHub、Twitter 等）

**方案：** 用 RackNerd 美国 VPS 作为抓取代理节点

```
aictl → 请求 VPS → VPS 抓网页 → 返回 markdown → aictl
```

**状态：** 方案保留，暂不实施。

---

## 本次涉及的所有文件变更

本会话改动的文件（含之前未提交的改动）：

| 文件 | 改动类型 | 说明 |
|---|---|---|
| `aictl/internal/tui/model.go` | 修改 | TUI 工具输出折叠 + esc 取消 + 工具计时 |
| `aictl/internal/tui/run.go` | 修改 | TUI 启动逻辑调整 |
| `aictl/internal/tui/tuiio.go` | 修改 | 取消函数注入 |
| `aictl/internal/tools/web_fetch.go` | 修改 | 缓存 + 行截断 |
| `aictl/internal/tools/web_search.go` | 新增 | web_search 工具 |
| `aictl/internal/tools/executor.go` | 修改 | 工具执行器增强 |
| `aictl/internal/tools/registry.go` | 修改 | 工具注册 |
| `aictl/internal/tools/bash.go` | 修改 | bash 工具增强 |
| `aictl/internal/tools/tool.go` | 修改 | 工具接口调整 |
| `aictl/internal/tools/tools_test.go` | 修改 | 测试更新 |
| `aictl/internal/agent/agent.go` | 修改 | agent 增强 |
| `aictl/internal/agent/loop.go` | 修改 | 主循环增强 |
| `aictl/internal/config/config.go` | 修改 | 配置增强 |
| `aictl/internal/config/config_test.go` | 修改 | 配置测试更新 |
| `aictl/cmd/chat.go` | 修改 | chat 命令调整 |
| `aictl/cmd/run.go` | 修改 | run 命令调整 |
| `aictl/go.mod` / `aictl/go.sum` | 修改 | 依赖更新 |

# Tool Routing 手动验收脚本

## 1. 前置检查

在 `apexion/` 目录执行：

```bash
go test ./...
go run . eval-tool-routing --strict
```

预期：全部通过。

## 2. 建议配置（便于观察）

在配置中打开调试：

```yaml
tool_routing:
  enabled: true
  enable_repair: true
  enable_fallback: true
  debug: true
```

## 3. 启动方式

```bash
go run .
```

每条用例执行后，可用 `/events 20` 查看 `tool_route` / `tool_repair` 事件。

## 4. 用例清单

### A. Codebase（高语义优先）

输入：
`请先快速理解这个仓库的架构，然后告诉我入口和关键模块。`

预期：
1. 优先出现 `repo_map`。
2. 后续出现 `symbol_nav` / `read_file`。
3. 不应先从 `web_search` 开始。

### B. Symbol 导航

输入：
`帮我找一下 runAgentLoop 在哪里定义、被哪些地方调用。`

预期：
1. 优先出现 `symbol_nav`。
2. 再用 `read_file` 做局部展开。

### C. 文档检索

输入：
`查一下 Go context 的最新官方文档和常见用法。`

预期：
1. 优先出现 `doc_context`。
2. 可跟随 `web_search` / `web_fetch` 深挖。

### D. Debug 路径

输入：
`这个模块报 panic，帮我定位根因。`

预期：
1. `symbol_nav` / `bash` 在前列。
2. 不应先走 git 工具。

### E. Git 路径

输入：
`先看 git status 和 diff，再告诉我现在能不能提交。`

预期：
1. `git_status`、`git_diff` 优先。
2. 路由 intent 为 `git`。

### F. System 路径

输入：
`我的磁盘快满了，帮我看下系统占用。`

预期：
1. 优先 `bash`。
2. 不应优先 `repo_map` / `web_search`。

### G. 工具名修复（name repair）

输入：
`用 ls 看下当前目录有哪些文件。`

预期：
1. 实际执行 `list_dir`（由别名修复）。
2. `/events` 中出现 `tool_repair`。

### H. 参数修复（arg repair）

输入：
`帮我用 read_file 读取 README，参数就用 path 字段。`

预期：
1. `path -> file_path` 自动修复。
2. 执行成功，出现 `tool_repair` 事件。

### I. 回退链路（fallback）

输入：
`查某个库文档（故意给一个不存在版本号）`

预期：
1. `doc_context` 失败后回退 `web_search` / `web_fetch`。
2. 输出中能看到 repair/fallback 提示前缀。

### J. 图像回合（MiniMax 文本图像桥接场景）

步骤：
1. 拖入一张图片。
2. 输入：`请看这张图并总结关键问题。`

预期：
1. 不应出现 `web_fetch` 直拉图片 URL 的误路由。
2. 出现图像桥接工具路径（MCP 相关）。
3. `/events` 出现 `tool_route`，包含 `filtered_tools` 中有 `web_fetch`（文本图像不直收时）。

## 5. 通过标准

1. 上述 10 条用例中，至少 9 条满足预期。
2. 所有错误都能看到明确 repair/fallback 轨迹（不是静默失败）。
3. 不出现连续重复失败卡死（应触发失败回路告警/停止）。

## 6. 失败排查

1. 检查 `tool_routing.enabled/enable_repair/enable_fallback` 是否开启。
2. 用 `/events 50` 查看 `tool_route` 和 `tool_repair`。
3. 若是图像问题，先 `/mcp` 确认图像理解 MCP 工具可用。


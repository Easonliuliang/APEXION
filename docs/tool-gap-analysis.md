# aictl 工具差距分析：对标 Claude Code / OpenCode / Codex

## 一、工具数量对比

| 产品 | 工具数 | 语言 |
|------|--------|------|
| **aictl** | 13 | Go |
| **Claude Code** | 17 | TypeScript (Bun 编译) |
| **OpenCode** (Go) | 12 | Go |
| **OpenCode** (TS) | 18+ | TypeScript |
| **Codex CLI** | 16+ | Rust |

## 二、工具矩阵对比

| 能力 | aictl | Claude Code | OpenCode | Codex |
|------|:-----:|:-----------:|:--------:|:-----:|
| 文件读取 | read_file | Read (图片/PDF/notebook) | view | read_file (indentation模式) |
| 文件写入 | write_file | Write | write | apply_patch |
| 文件编辑 | edit_file | Edit (replace_all) | edit (9种匹配策略) | apply_patch (自定义diff) |
| Shell 执行 | bash | Bash (后台/沙箱) | bash (持久化会话) | shell/exec_command (3层+PTY) |
| 文件搜索 | glob | Glob (file-index.node) | glob (rg+doublestar) | grep_files (rg) |
| 内容搜索 | grep | Grep (ripgrep.node, 3模式) | grep (rg+Go降级) | grep_files |
| 目录列表 | list_dir | 无 (用Bash) | ls | list_dir (递归深度) |
| Git 操作 | 4个 (status/diff/commit/push) | 无 (用Bash) | 无 (用bash) | 无 (用shell) |
| 网页获取 | web_fetch | WebFetch (server tool) | fetch | 无 (用shell curl) |
| 网络搜索 | web_search | WebSearch (server tool) | websearch (Exa) | web_search (cached/live) |
| **子Agent** | **无** | Task | agent | spawn_agent/wait/close |
| **任务跟踪** | **无** | TodoWrite | todowrite/todoread | update_plan |
| **LSP 诊断** | **无** | 无 (内部用) | diagnostics + lsp (9种操作) | 无 |
| **用户提问** | **无** | 无 (系统级) | question | request_user_input |
| **批量并行** | **无** | 无 (LLM原生并行) | batch (25个) | 并行agent |
| **多文件补丁** | **无** | 无 | patch (原子化) | apply_patch |
| **持久化Shell** | **无** | 无 | shell (环境持久) | exec_command (PTY) |
| **计划模式** | **无** | EnterPlanMode | Plan模式 | update_plan |
| Notebook | 无 | NotebookEdit | 无 | 无 |
| 代码搜索 | 无 | 无 | sourcegraph | search_tool_bm25 |
| JS REPL | 无 | 无 | 无 | js_repl |
| 技能系统 | 无 | Skill | skill | 无 |
| MCP 集成 | 无 | ToolSearch | mcp-tools | mcp/mcp_resource |
| 沙箱隔离 | 无 | 应用层审批 | 应用层审批 | OS级 (Seatbelt/Landlock) |

## 三、高收益缺失工具排名

### Tier 1 — 收益极大，三家竞品都有

#### 1. 子 Agent (Task/spawn_agent)

**三家都有，aictl 没有。这是最大的差距。**

| 产品 | 实现 |
|------|------|
| Claude Code | `Task` — 启动子agent，支持多种角色（Explore/Plan/general-purpose），可后台运行 |
| OpenCode | `agent` — 子agent只能用只读工具（Glob/Grep/View），支持并行 |
| Codex | `spawn_agent` + `wait` + `close_agent` — 完整生命周期管理，角色配置，深度限制 |

**为什么重要**：
- 复杂任务可以并行处理（如同时搜索多个文件、研究多个问题）
- 保护主上下文窗口，子agent的大量搜索结果不会塞满主对话
- LLM 越来越擅长使用子agent进行任务分解

**aictl 实现建议**：
```
优先级: P0
复杂度: 中等
方案: 创建一个 task 工具，复用现有的 agent.Run()，
     子agent仅给只读工具（glob/grep/read_file/list_dir/web_fetch）
```

---

#### 2. 任务跟踪 (TodoWrite)

**Claude Code 和 OpenCode 都有，Codex 有 update_plan。**

| 产品 | 实现 |
|------|------|
| Claude Code | `TodoWrite` — 全量替换模式，content + status (pending/in_progress/completed) |
| OpenCode | `todowrite` + `todoread` — 分读写两个工具，按sessionID索引 |
| Codex | `update_plan` — 结构化步骤追踪，每步有 step + status |

**为什么重要**：
- 多步骤任务时 LLM 不会"忘记"后续步骤
- 用户可以看到任务进度
- 避免 LLM 在长对话中迷失方向

**aictl 实现建议**：
```
优先级: P0
复杂度: 低
方案: 内存中维护一个 []TodoItem，两个工具 todo_write/todo_read
     todo_write 接收完整列表（全量替换），todo_read 无参数返回当前列表
     TUI 中可以渲染一个简单的进度展示
```

---

### Tier 2 — 收益大，部分竞品有

#### 3. LSP 诊断 (diagnostics)

**OpenCode 有，其他没暴露给 LLM。**

| 产品 | 实现 |
|------|------|
| OpenCode (Go) | `diagnostics` — 聚合多个LSP客户端结果，按严重级别排序 |
| OpenCode (TS) | `lsp` — 9种操作：goToDefinition, findReferences, hover, documentSymbol 等 |
| Claude Code | 内部使用 LSP（Edit/Write 后通知），但不暴露为工具 |
| Codex | 无 |

**为什么重要**：
- 编辑代码后立即知道有没有编译错误，不用跑 `go build`
- 跳转定义、查找引用让 LLM 更精准地理解代码结构
- 大幅减少"改了之后编译不过"的来回循环

**aictl 实现建议**：
```
优先级: P1
复杂度: 高（需要集成 LSP 客户端）
方案: 先做最简版——edit/write 后自动跑 go vet/go build 捕获错误
     后续再考虑完整 LSP 集成（gopls）
```

---

#### 4. 用户提问 (question/request_user_input)

**OpenCode 和 Codex 有。**

| 产品 | 实现 |
|------|------|
| OpenCode (TS) | `question` — 标题 + 问题 + 选项列表，用户选择或自定义输入 |
| Codex | `request_user_input` — 结构化多选问题 |

**为什么重要**：
- LLM 在执行中遇到歧义时可以主动问用户，而不是猜测
- 比中断整个流程然后重新开始好得多
- 减少错误操作

**aictl 实现建议**：
```
优先级: P1
复杂度: 中等（需要 TUI 配合渲染选项）
方案: 工具返回问题和选项 → TUI 渲染选择界面 → 用户选择后结果传回 LLM
     复用现有的 confirm 机制扩展
```

---

#### 5. 多文件原子补丁 (patch)

**OpenCode 和 Codex 有。**

| 产品 | 实现 |
|------|------|
| OpenCode | `patch` — 自定义 diff 格式，支持 Add/Update/Delete File |
| Codex | `apply_patch` — 自定义精简 diff，Lark 语法定义 |

**为什么重要**：
- 重构时需要同时改多个文件（如改接口 + 所有实现）
- 原子性保证：要么全部成功，要么全部回滚
- 减少"改了一半出错"的情况

**aictl 实现建议**：
```
优先级: P2
复杂度: 中等
方案: 解析自定义 diff 格式，预检查所有文件，
     全部通过后一次性写入。失败则回滚。
```

---

#### 6. 持久化 Shell

**OpenCode 有。**

当前 aictl 的 bash 工具每次执行都是新进程，环境变量和目录切换不保持。

**为什么重要**：
- `cd /some/dir && export FOO=bar` 后面的命令看不到这些状态
- 需要用户每次都写完整路径和重复设置环境变量

**aictl 实现建议**：
```
优先级: P2
复杂度: 中等
方案: 维护一个长期运行的 shell 进程（如 bash -i），
     通过 stdin/stdout 管道通信
```

---

### Tier 3 — 有价值但优先级低

#### 7. 计划模式 (EnterPlanMode)

**Claude Code 和 OpenCode 都有。** LLM 在编码前先制定计划，用户审批后再执行。减少返工。

```
优先级: P2 — 可通过 prompt engineering 部分实现
```

#### 8. 批量并行工具执行 (batch)

**OpenCode TS 版有。** 一次调用执行最多 25 个工具。目前 LLM 原生已支持并行 tool_use，这个主要是给不支持并行的模型用的。

```
优先级: P3 — 如果主要用 Claude/GPT 等支持并行调用的模型，优先级低
```

#### 9. Sourcegraph 代码搜索

**OpenCode 独有。** 搜索公共代码仓库，找参考实现。

```
优先级: P3 — 可以通过 web_search 部分替代
```

---

## 四、实现路线图建议

### 阶段一：核心能力补齐（2 个工具）

| 工具 | 工作量 | 说明 |
|------|--------|------|
| **task** (子agent) | 3-5天 | 复用 agent.Run()，限制子agent只用只读工具 |
| **todo_write/todo_read** | 1-2天 | 内存 map + 两个简单工具 |

**预期收益**：LLM 可以分解复杂任务、并行搜索、追踪多步骤进度。这是从"能用"到"好用"的关键跳跃。

### 阶段二：代码质量提升（2 个改进）

| 改进 | 工作量 | 说明 |
|------|--------|------|
| **编辑后自动诊断** | 2-3天 | edit/write 后自动 go vet/build，错误反馈给 LLM |
| **用户提问工具** | 2-3天 | question 工具 + TUI 渲染 |

**预期收益**：减少编译错误的来回循环，LLM 遇到歧义时不再盲猜。

### 阶段三：体验优化（按需）

| 改进 | 工作量 | 说明 |
|------|--------|------|
| 持久化 Shell | 3-5天 | 维护长期 shell 进程 |
| 多文件 patch | 3-5天 | 原子化多文件编辑 |
| 计划模式 | 2-3天 | 编码前先出方案，用户审批 |

---

## 五、一句话总结

aictl 当前的工具集覆盖了**基础文件操作和搜索**，但缺少**子Agent、任务跟踪、LSP诊断、用户提问**四个让 AI 编程助手从"能跑"变成"好用"的关键工具。优先补齐 **子Agent** 和 **任务跟踪**，投入产出比最高。

---

## 附录：各产品底层技术对比

| 维度 | aictl | Claude Code | OpenCode | Codex |
|------|-------|-------------|----------|-------|
| 搜索引擎 | Go filepath.Walk + regexp | 自研 ripgrep.node + file-index.node (原生模块) | ripgrep 优先 + Go regexp 降级 | ripgrep |
| HTML→MD | html-to-markdown 库 | 类似 | goquery + html-to-markdown | 无 (用 shell) |
| 编辑策略 | 精确字符串匹配 | 精确匹配 + Unicode 处理 + XML 防注入 | 9种递进式匹配策略 (TS版) | 自定义 diff 格式 (Lark 语法) |
| 沙箱 | 无 | 应用层审批 | 应用层权限 + doom_loop 检测 | OS级 (Seatbelt/Landlock/seccomp) |
| 缓存 | web_fetch 15分钟 LRU | web_fetch 15分钟 50MB LRU | 无 | web_search cached 模式 |
| 原生模块 | 无 | ripgrep.node, file-index.node, image-processor.node, tree-sitter.wasm | 无 | Rust 原生 |

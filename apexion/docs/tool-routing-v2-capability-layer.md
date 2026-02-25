# Tool Routing V2 稳定化方案（能力层 + 策略层 + 执行层）

## 1. 背景与问题
当前路由已经有明显进步：
- 已有意图分类与打分路由（`internal/router/router.go`）。
- 已有高语义工具优先（`repo_map/symbol_nav/task/doc_context/context7/github`）。
- 已有懒连接与失败隔离（`internal/agent/mcp_lazy.go` + `internal/mcp/client.go`）。

但仍存在结构性风险：
- 工具选择主要由启发式权重驱动，缺少“正式能力声明”。
- 新增工具/模型时容易继续堆 if-else，长期维护成本高。
- 高频查询（例如“定义在哪”）仍依赖模型先决策工具，导致额外一轮模型往返。

本方案目标：在不推翻现有高语义策略的前提下，补齐产品级稳定层。

## 2. 设计目标
1. 稳定性：避免“句式补丁式演进”。
2. 低延迟：能力层判定必须本地毫秒级，不新增网络回调。
3. 可演进：新增工具只补声明，不改大量路由逻辑。
4. 可观测：可量化评估每次策略变更的收益与回归。
5. 可回滚：新旧路由并行评估，不达标不切流。

## 3. 非目标
1. 不在本阶段引入复杂在线学习路由器。
2. 不在本阶段引入必须依赖 Token/API Key 的 GitHub 数据链路。
3. 不改变当前用户交互语义（命令、文案、会话流程保持兼容）。

## 4. 三层架构

### 4.1 能力层（Capability Layer）
职责：定义“工具能做什么、何时可用、代价多大、风险多高”。

核心结构（建议新增）：
- 文件：`internal/router/capabilities.go`
- 配置覆盖：`tool_capabilities`（可选）

建议字段：
- `name`: 工具名。
- `domains`: 适配意图集（codebase/debug/research/git/system/vision）。
- `semantic_level`: high/medium/primitive。
- `risk`: read/write/execute/network。
- `requires`: 前置条件集合。
- `cost_class`: low/medium/high（估算执行/连接成本）。
- `latency_hint_ms`: 经验延迟标签。
- `supports_parallel`: 是否适合并行。
- `deterministic_for`: 可直通的任务类型（例如 `symbol_lookup`）。
- `provider_constraints`: 模型/供应商约束（例如图像输入要求）。
- `degrade_policy`: 失败时回退目标（例如 `symbol_nav -> grep`）。

`requires` 建议支持：
- `model.image_input=true`
- `mcp.server=xxx`
- `filesystem.read=true`
- `network=true`
- `permission=<tool>`

说明：
- 这是“声明数据”，不是执行逻辑。
- 构建候选集时只做本地过滤和打分，不触发额外模型请求。

### 4.2 策略层（Policy Layer）
职责：根据用户输入、能力层、上下文信号，输出 `primary + fallback`。

保留并增强现有逻辑：
- 现有入口：`internal/router/router.go`
- 现有意图识别：`internal/router/intent.go`
- 现有 profile：`internal/router/profiles.go`

升级为两阶段：
1. `Eligibility Filter`：按能力声明过滤不可用工具。
2. `Policy Scoring`：对可用工具做排序。

打分建议（示例）：
- 意图匹配：+40
- 语义层级：high +25 / medium +12 / primitive +4
- 前置条件完全满足：+20
- 成本惩罚：low 0 / medium -6 / high -15
- 风险惩罚（在非必要场景）：write -8 / execute -12
- 历史成功加权：近 N 轮工具成功率作为 +0~+10
- 已知误路由惩罚：例如图像轮禁用 `web_fetch`（保留现有 hard gate）

#### 直通策略（减少 1 轮模型回合）
当满足以下条件时，允许跳过“模型先选工具”这一步，直接调用工具：
- 任务类型在 `deterministic_for` 中。
- 置信度超过阈值（例如 `>= 0.85`）。
- 工具前置条件满足且健康状态正常。

第一批建议只开两个直通：
1. `symbol_lookup` -> `symbol_nav`
2. `repo_overview` -> `repo_map`

这样可明显减少“先思考再调用”耗时，同时风险可控。

### 4.3 执行层（Execution Layer）
职责：工具执行、懒连接、失败隔离、熔断与回退。

现状基础（已具备）：
- 懒连接：`internal/agent/mcp_lazy.go`
- 连接状态与降级：`internal/mcp/client.go`

建议补强：
- `ToolHealth`：按工具维度维护短期健康分。
- `Circuit Breaker`：连续失败达到阈值后临时降权/短路。
- `Cooldown`：冷却窗口内不反复触发同一失败工具。
- `Fallback Chain`：按能力声明执行固定回退链。

## 5. 与现有高语义策略的关系
不会降级，采用“兼容迁移”：
1. 保留 `preferredTools + scoreTool` 作为旧策略。
2. 新增能力层先只参与过滤与记录，不改变最终执行。
3. 新旧并跑输出 diff 指标。
4. 达标后再切换到“能力层+策略层”的主路径。

这意味着你现在做过的高语义优化会被吸收，而不是被推翻。

## 6. 配置设计
建议在 `config.yaml` 增加：

```yaml
tool_routing:
  enabled: true
  max_candidates: 6
  enable_repair: true
  enable_fallback: true
  debug: false
  strategy: legacy          # legacy | hybrid | capability_v2
  deterministic_fastpath: true
  fastpath_confidence: 0.85
  shadow_eval: true
  shadow_sample_rate: 1.0
  circuit_breaker:
    enabled: true
    fail_threshold: 3
    cooldown_sec: 120
```

可选能力覆盖：

```yaml
tool_capabilities:
  symbol_nav:
    deterministic_for: [symbol_lookup]
    cost_class: low
  repo_map:
    deterministic_for: [repo_overview]
    cost_class: medium
```

## 7. 观测与评估

### 7.1 日志字段（新增）
- `route.strategy`
- `route.intent`
- `route.eligible_tools`
- `route.primary_tools`
- `route.fastpath_used`
- `route.fastpath_confidence`
- `route.shadow_top1`
- `route.shadow_diff`
- `tool.exec_latency_ms`
- `tool.exec_result`
- `tool.health_score`

### 7.2 核心指标
1. `Top1 命中率`：首选工具是否命中期望。
2. `平均工具步数`：每个任务平均工具调用次数。
3. `首响耗时`：从用户输入到首个工具开始。
4. `端到端耗时 P50/P95`。
5. `工具失败率` 与 `回退成功率`。
6. `MCP 连接失败对主流程影响率`（应接近 0）。

### 7.3 验收门槛（建议）
- Top1 命中率不低于现网，且提升 >= 5%。
- 平均工具步数下降 >= 10%。
- P95 端到端耗时不升高，目标下降 >= 8%。
- 关键场景失败率不高于现网。

## 8. 实施分期

### Phase 0（1 天）：能力层建模，不改变行为
- 新增 `Capability` 结构与默认注册表。
- 从现有 `DefaultProfile` 映射出初版能力声明。
- 日志输出能力过滤信息。

涉及文件：
- `internal/router/types.go`
- `internal/router/profiles.go`
- `internal/router/capabilities.go`（新增）
- `internal/router/router.go`

### Phase 1（1~2 天）：Hybrid 模式 + Shadow 对比
- `strategy=hybrid`：执行仍走旧策略，旁路跑新策略。
- 写入 diff 指标。
- 扩充 `docs/tool-routing-eval-dataset.json` 增加稳定性样本。

涉及文件：
- `internal/router/router.go`
- `cmd/eval_tool_routing.go`
- `docs/tool-routing-eval-dataset.json`

### Phase 2（1 天）：小范围切流 + 快速回滚
- 对高置信场景开启 fastpath（只开 symbol/repo 两类）。
- 观察 1 天指标。
- 如有回归，切回 `legacy`（配置级回滚）。

涉及文件：
- `internal/agent/loop.go`
- `internal/router/router.go`
- `internal/config/config.go`

### Phase 3（持续）：执行层稳态增强
- 接入工具健康分与熔断。
- 完整 fallback chain。
- 持续维护能力声明，而不是加句式补丁。

涉及文件：
- `internal/agent/tool_repair.go`
- `internal/agent/mcp_lazy.go`
- `internal/mcp/client.go`

## 9. 风险与控制
1. 风险：能力声明过严导致可用工具减少。
- 控制：先 shadow，再逐步切流；设置 `legacy` 一键回滚。

2. 风险：fastpath 误判导致错误工具直通。
- 控制：高阈值、白名单场景、保底 fallback。

3. 风险：MCP 波动影响结果稳定。
- 控制：熔断 + 冷却 + 失败隔离，不阻断主进程。

## 10. 与 OpenCode/Codex 对齐点
对齐点：
- 工具 schema 化与能力约束。
- 工具失败修复与回退。
- 模型能力门控（图像等）。
- MCP 失败隔离。

我们的差异化（保留优势）：
- 更轻量的 Go 单二进制路线。
- 已实现 lazy connect，能更好控制资源占用。
- 可在不引入 Token 依赖的前提下保持高语义路由能力。

## 11. 决策建议
建议立即进入 Phase 0 + Phase 1。
- 原因：不改现网行为，风险最低。
- 收益：先把“补丁式路由”升级到“声明式路由骨架”。

完成 Phase 1 后再决定 fastpath 切流阈值。

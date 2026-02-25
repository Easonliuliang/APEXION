# Tool Routing 离线评测

使用内置离线评测命令快速验证 Tool Router/Repair 策略是否回归。

## 数据集

- 默认数据集：`docs/tool-routing-eval-dataset.json`
- 当前版本覆盖意图：
  - codebase
  - debug
  - research
  - git
  - system
  - vision

## 运行方式

在 `apexion/` 目录下执行：

```bash
go run . eval-tool-routing
```

严格模式（有失败即返回非零）：

```bash
go run . eval-tool-routing --strict
```

JSON 输出（便于 CI 解析）：

```bash
go run . eval-tool-routing --json
```

指定数据集：

```bash
go run . eval-tool-routing --dataset docs/tool-routing-eval-dataset.json
```

限制路由候选上限（模拟线上配置）：

```bash
go run . eval-tool-routing --max-candidates 8
```

指定路由策略（`legacy | hybrid | capability_v2`）：

```bash
go run . eval-tool-routing --strategy capability_v2
```

开启 shadow 评估（用于对比主路径与影子路径 Top1 差异）：

```bash
go run . eval-tool-routing --strategy hybrid --shadow-eval
```

控制 shadow 采样率（`0~1`）：

```bash
go run . eval-tool-routing --strategy hybrid --shadow-eval --shadow-sample-rate 0.3
```

开启 deterministic fastpath 评估：

```bash
go run . eval-tool-routing --strategy capability_v2 --deterministic-fastpath --fastpath-confidence 0.85
```

## 指标说明

- `Intent`: 意图分类准确率
- `Top hit`: 期望工具集合在 Top-K 的命中率
- `Contain`: 必须包含工具集合的命中率
- `Filtered`: 期望被硬过滤工具的命中率（例如图像回合的 `web_fetch`）
- `Shadow diff(top1)`: 影子路由与主路由 Top1 不一致的 case 数（仅 shadow 打开时显示）
- `Fastpath hits`: 命中 deterministic fastpath 的 case 数

## 扩展建议

新增 case 时，建议同时补：

1. `expected_intent`
2. `expected_top_any` + `expected_top_k`
3. `expected_filtered`（如有硬过滤预期）

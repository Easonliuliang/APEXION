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

## 指标说明

- `Intent`: 意图分类准确率
- `Top hit`: 期望工具集合在 Top-K 的命中率
- `Contain`: 必须包含工具集合的命中率
- `Filtered`: 期望被硬过滤工具的命中率（例如图像回合的 `web_fetch`）

## 扩展建议

新增 case 时，建议同时补：

1. `expected_intent`
2. `expected_top_any` + `expected_top_k`
3. `expected_filtered`（如有硬过滤预期）


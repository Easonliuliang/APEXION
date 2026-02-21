# aictl 开源路线图

## 当前状态

- 16 个工具（文件操作、git、web、todo、子 agent）
- Bubbletea TUI（折叠工具输出、子 agent 进度、esc 取消）
- 多 provider 支持（DeepSeek / OpenAI / Anthropic 热切换）
- Session 持久化（SQLite）
- 上下文自动压缩

---

## 第一步：补体验短板（优先级最高）

### 1.1 用户提问工具 (question)

**状态：** 未开始
**难度：** 中（~200 行，需 TUI 配合）

LLM 遇到歧义时主动问用户，而不是猜测。需要：
- `tools/question.go` — 工具定义，接收问题 + 选项列表
- 扩展 `Confirmer` 接口或新增 `Questioner` 接口 — 返回用户选择（不只是 bool）
- TUI 渲染选项列表 — 用户上下键选择或输入自定义答案

参考：OpenCode 的 `question` 工具，Codex 的 `request_user_input`

### 1.2 edit_file 模糊匹配降级

**状态：** 未开始
**难度：** 中

当前 `edit_file` 要求 `old_string` 精确匹配，LLM 经常因为空格/缩进/换行差异匹配失败。改进：
- 精确匹配失败后，尝试去掉首尾空白再匹配
- 再失败，尝试忽略空行差异
- 仍失败，返回最相似片段的 diff 帮助 LLM 修正

参考：OpenCode TS 版有 9 种递进式匹配策略

### 1.3 System prompt 去废话

**状态：** 部分完成（已精简验证规则）
**难度：** 低

问题：LLM 在每次工具调用前插一句 "让我查看..."、"现在让我..."。需要在 prompt 中明确：
- 不要在工具调用前写解释性文字
- 连续工具调用时不要插入过渡文字
- 只在最终结果时输出文字

---

## 第二步：差异化打磨

### 2.1 定位明确

```
aictl — 最轻量的 AI 编码助手
单个 Go 二进制，零依赖，原生支持 DeepSeek/OpenAI/Anthropic 热切换
```

卖点：
- 对比 Claude Code：不绑定 Anthropic，支持便宜模型（DeepSeek）
- 对比 OpenCode：更精简，架构更清晰
- 对比 Codex：不需要 Rust 工具链，安装简单

### 2.2 安装体验

确保以下安装方式可用：
```bash
# Go 安装
go install github.com/aictl/aictl@latest

# 或下载二进制
curl -fsSL https://github.com/aictl/aictl/releases/download/v0.3.0/aictl-darwin-arm64 -o aictl
chmod +x aictl
```

考虑加 Homebrew tap（后续）。

### 2.3 中英文双语

- System prompt 默认英文（LLM 英文理解更好）
- 用户交互自动检测语言，中文用户中文回复
- README 英文为主，附中文简介

---

## 第三步：开源发布

### 3.1 README.md

结构：
```
# aictl

一句话介绍 + badge

## Features（核心功能截图/GIF）

## Quick Start（3 行搞定）

## Supported Providers（表格）

## Architecture（简图）

## Contributing

## License
```

### 3.2 演示 GIF

用 [vhs](https://github.com/charmbracelet/vhs) 录制 30 秒演示：
- 启动 aictl
- 让 AI 读文件 + 编辑 + 运行测试
- 展示工具折叠效果

### 3.3 清理

- [ ] `.gitignore` 完善（二进制、.env、.DS_Store）
- [ ] 移除硬编码的测试路径
- [ ] `go.mod` 的 module path 确认（是否用 github.com/aictl/aictl）
- [ ] LICENSE 文件（MIT）
- [ ] GitHub Actions CI（build + test）

### 3.4 发布渠道

| 渠道 | 内容 |
|------|------|
| GitHub | repo + release + README |
| X (Twitter) | 英文推文 + 演示 GIF |
| 小红书 | 中文图文 + 功能截图 |
| V2EX | 技术帖 + 架构分享 |
| Hacker News | Show HN 帖 |

---

## 时间估算

| 阶段 | 预估 | 说明 |
|------|------|------|
| 第一步 | 2-3 天 | 用户提问 + edit 容错 + prompt 调优 |
| 第二步 | 1 天 | 安装脚本 + 定位文案 |
| 第三步 | 1 天 | README + GIF + 清理 + 发布 |
| **总计** | **4-5 天** | 可以更快，看投入时间 |

---

## 附：完成后的工具清单（目标 17-18 个）

| # | 工具 | 状态 |
|---|------|------|
| 1 | read_file | ✅ |
| 2 | edit_file | ✅（待加模糊匹配） |
| 3 | write_file | ✅ |
| 4 | bash | ✅ |
| 5 | glob | ✅ |
| 6 | grep | ✅ |
| 7 | list_dir | ✅ |
| 8 | git_status | ✅ |
| 9 | git_diff | ✅ |
| 10 | git_commit | ✅ |
| 11 | git_push | ✅ |
| 12 | web_fetch | ✅ |
| 13 | web_search | ✅ |
| 14 | todo_write | ✅ |
| 15 | todo_read | ✅ |
| 16 | task | ✅ |
| 17 | **question** | ⬜ 第一步 |

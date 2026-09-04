# WorkBuddy 风格记忆系统重写实施计划

## 仓库研究结论

### 现有记忆系统架构（5 个文件 + 1 个集成层）

| 模块 | 文件 | 核心能力 | 问题 |
|---|---|---|---|
| 主 Store | [store.go](file:///workspace/internal/memory/store.go) | SQLite FTS5 + 6种 MemoryType + MEMORY.md/USER.md 文件、命令信任级别 | 三种 Store 并存、职责混淆 |
| FTS Store | [fts_store.go](file:///workspace/internal/memory/fts_store.go) | 会话级消息存储、BM25 排序、重要性、洞察、清理策略 | 与 store.go 表结构重复，各自一套 schema |
| Enhanced FTS | [enhanced_fts_store.go](file:///workspace/internal/memory/enhanced_fts_store.go) | LRU 缓存、同义词、停用词、重要性衰减、内容去重（hash）、语义相似度 Jaccard、记忆关联链接 | 功能臃肿、与 FTSStore 无法互通、未被任何调用方引用 |
| Snapshot | [snapshot_manager.go](file:///workspace/internal/memory/snapshot_manager.go) | 冻结快照（frozen/latest 双缓冲保护前缀缓存）、字符截断压缩 | 字符限制硬编码、压缩策略过于简陋 |
| Tools | [tool.go](file:///workspace/internal/memory/tool.go) | Cobra CLI 命令 + OpenAI 风格 ToolFunction（8 个 operation） | agent_memory/user_memory 只支持 read |
| 集成层 | [memory_integration.go](file:///workspace/internal/memory/integration/memory_integration.go) | 正则抽取偏好、BuildContext、PeriodicRecall、CompactSessionMemories | 抽取精度低（纯正则）、无学习周期 |

### 调用方依赖关系

```
cmd/magic/tools.go 注册 (memory_store / memory_recall)
  └─> ToolFunction.Execute() [tool.go]
        └─> Store (Store/Recall/Search/...)

cortex.Manager [cortex/integration.go]
  ├─> SnapshotManager : Load/OnTurnStart/RefreshSnapshot/GetMemoryForPrompt
  └─> FTSStore        : Search/AddInsight/GetStats   (EnhancedFTSStore 完全未接入)

agent.Agent [agent/agent.go]
  └─> cortexManager (通过 manager 使用记忆)

integration.MemoryIntegration [integration/memory_integration.go]
  └─> Store + 正则偏好抽取 + BuildContext
```

### WorkBuddy 记忆系统核心设计理念

参考两篇公开资料的设计：

1. **三层记忆结构**（不是 RAG 也不是长上下文）
   - **短期记忆 (Short-term)**：会话内上下文，结束即丢弃
   - **工作记忆 (Working)**：每日文件 `YYYY-MM-DD.md`，自动追加、自动归档
   - **长期记忆 (Long-term)**：`MEMORY.md`（结论性记忆）+ SQLite FTS5 检索（历史回溯）

2. **隐藏规则文件（元记忆）**
   - `IDENTITY.md` — Agent 身份、角色、默认工作目录
   - `SOUL.md` — 人格、工作风格、沟通方式
   - `USER.md` — 用户画像（偏好、身份、联系方式、项目）

3. **D2 记忆层五项技术**
   - **指数衰减 (memory-decay)**：重要性随访问频率 + 时间衰减（类似人类遗忘曲线）
   - **四层压缩**：短期/工作/长期/归档，层层压缩避免膨胀
   - **版本控制 (memory-git)**：所有记忆文件纳入 git 跟踪，可回溯、可对比
   - **关系图谱 (memory-graph)**：记忆间的 reference/follows/related 关联
   - **智能去重**：内容 hash + 语义相似度双重去重

4. **四个学习周期（参考 biological memory 固化模型）**
   - `Reflect`（每对话后）：日志化、抽取观察、提取事实
   - `Daily`（每日结束）：概念形成、关联、成熟观察升级为长期记忆
   - `Weekly`（每周）：校准重要性、跨概念泛化、轻度清理标记
   - `Archive`（月度/阈值）：长期记忆归档压缩

5. **核心设计哲学**："不是让 Agent 记住所有事情，而是让它**知道去哪里找需要的信息**"

## 文件和模块变更

### 新增文件

| 路径 | 内容 |
|---|---|
| `internal/memory/layer/short_term.go` | 短期记忆层：会话内存 ring buffer，负责 turn 级上下文窗口 |
| `internal/memory/layer/working.go` | 工作记忆层：每日 YYYY-MM-DD.md 自动追加 + 当日检索 + 自动归档到长期 |
| `internal/memory/layer/long_term.go` | 长期记忆层：MEMORY.md 的结构化段落管理 + 字符预算 + LLM 压缩器 |
| `internal/memory/layer/meta_files.go` | 元文件管理层：IDENTITY.md / SOUL.md / USER.md 的 CRUD + 校验 + 默认模板 |
| `internal/memory/decay/decay.go` | 指数衰减器：Ebbinghaus 遗忘曲线公式、boost（访问/引用可提升）、批量 apply |
| `internal/memory/compress/compressor.go` | 四层压缩器：short→working→long→archive，每种策略不同（摘要/重要性筛选/结论提取） |
| `internal/memory/graph/graph.go` | 关系图谱：记忆节点的 follows/reference/related 边、邻接检索、图谱可视化导出 |
| `internal/memory/gitstore/version.go` | Memory Git 版本控制：auto commit / diff / revert / blame（基于 go-git 或 shell git） |
| `internal/memory/cycle/reflect.go` | Reflect 学习周期：对话结束自动抽取 facts/preferences/decisions/errors |
| `internal/memory/cycle/daily.go` | Daily 学习周期：工作记忆 → 长期记忆的整合 + 归档旧每日文件 |
| `internal/memory/cycle/weekly.go` | Weekly 学习周期：重要性校准 + 跨会话概念泛化 + 清理标记 |
| `internal/memory/manager.go` | 统一入口 MemoryManager：编排三层 + 元文件 + 衰减 + 压缩 + 图谱 + 学习周期 |

### 重写/重构文件

| 路径 | 变更 |
|---|---|
| `internal/memory/store.go` | **大重构**：剥离 6 种 MemoryType 到三层模型，仅保留长期记忆的 SQLite schema，统一 FTS 索引，移除 command_history（迁到 learn/manager），删除 MEMORY.md/USER.md 文件读写（迁到 layer/long_term + layer/meta_files） |
| `internal/memory/fts_store.go` | **移除**：功能合并到 store.go 的 FTS 实现（短期/工作/长期统一表 + type 字段区分层级） |
| `internal/memory/enhanced_fts_store.go` | **移除**：缓存 → manager 内部；同义词/停用词 → store.go 检索 pipeline；衰减 → decay/；去重 → 内置去重器；相似度/链接 → graph/ |
| `internal/memory/snapshot_manager.go` | **重写**：保留 frozen 前缀缓存机制，但数据源改为 MemoryManager 的 GetContext()；支持 daily/meta/identity 多份快照 |
| `internal/memory/tool.go` | **重写**：CLI + ToolFunction 新 operation：`reflect` / `daily` / `weekly` / `archive` / `graph_query` / `meta_read` / `meta_write` / `revert` |
| `internal/memory/integration/memory_integration.go` | **重写**：对接 MemoryManager；正则抽取保留为 Reflect 的 fallback；BuildContext 改为三层召回 + 元文件拼接 |

### 修改调用方（最小侵入适配）

| 路径 | 变更 |
|---|---|
| `internal/cortex/integration.go` | cortex.Manager 用 MemoryManager 替换 Snapshot + FTSStore；保留相同方法签名 |
| `cmd/magic/tools.go` | 新增 `memory_reflect` / `memory_daily` / `memory_weekly` / `memory_graph` / `memory_meta` 工具注册 |
| `cmd/magic/chat.go` | 更新 MEMORY 系统提示词说明，明确三层记忆和学习周期 |

## 依赖顺序实施步骤

1. **核心基础设施层（无对外依赖，可并行）**
   - 1a 编写 `decay/decay.go`：Ebbinghaus 曲线、批量 apply、boost API、单元测试
   - 1b 编写 `compress/compressor.go`：四层压缩策略接口 + 三个实现（simple/rule-based/llm-optional）
   - 1c 编写 `gitstore/version.go`：git init/commit/diff/revert 封装（优先调用系统 git）
   - 1d 编写 `graph/graph.go`：节点/边模型 + SQLite 表（mem_links）+ 邻接查询 + 图谱导出

2. **元文件管理层**
   - 2a 编写 `layer/meta_files.go`：IDENTITY/SOUL/USER 模板 + 读写 + schema 校验 + 默认值
   - 2b 确保初始化时自动创建缺失的元文件

3. **三层记忆实现（按依赖顺序：短期→工作→长期）**
   - 3a `layer/short_term.go`：turn ring buffer（默认 50 turn）、按 session 分桶、按 role 过滤
   - 3b `layer/working.go`：每日 YYYY-MM-DD.md 追加、当日全文搜索、跨日跳转、7 天自动归档触发
   - 3c `layer/long_term.go`：MEMORY.md 结构化分段（标题 → 段落 ID map）、字符预算、append 超预算时调用 compressor、版本化写入

4. **学习周期实现**
   - 4a `cycle/reflect.go`：每对话结束调用；输入 conversation 历史 → 抽取 facts/decisions/errors/preferences → 写入 working + 候选长期
   - 4b `cycle/daily.go`：每日结束触发；将当周工作记忆整合 → 结论写入长期记忆 → 旧 daily 文件标记归档 → git commit
   - 4c `cycle/weekly.go`：每周触发；decay 校准、低重要性记忆清理标记、跨记忆图谱链接发现

5. **统一 MemoryManager 编排**
   - 5a `manager.go`：New → init 所有子模块 → 提供 Store/Recall/Search/Reflect/Daily/Weekly/Revert/GraphQuery/BuildContext 统一 API
   - 5b 注册 hook：在 Manager 中将 reflect 挂到对话结束、daily 挂到日期切换、weekly 挂到 cron

6. **重构旧文件（向后兼容）**
   - 6a 重写 `store.go`：保留 `Store/Recall/Search/List/Delete/Update` 方法，但内部委托给 MemoryManager 的长期层 + 工作层
   - 6b 重写 `snapshot_manager.go`：frozen/latest 快照改为从 manager 拉取 assembled prompt context
   - 6c 重写 `tool.go`：新 operation 映射到 manager 方法，保留旧 operation 作为兼容别名
   - 6d 重写 `integration/memory_integration.go`：BuildContext → manager.BuildContext；ExtractAndStore → cycle/reflect

7. **调用方适配**
   - 7a `internal/cortex/integration.go`：Manager.Snapshot / FTSMemory 统一替换为 MemoryManager 引用
   - 7b `cmd/magic/tools.go` + `chat.go`：新工具注册 + 提示词更新

8. **清理旧文件**
   - 8a 删除 `fts_store.go`、`enhanced_fts_store.go`
   - 8b 移除 store.go 中的 command_history（如果 learn/manager 已有对应功能则迁过去，否则保留）

9. **验证与回归**
   - 9a 编写单元测试：每层 CRUD + 衰减公式 + 压缩边界 + 图谱邻居查询
   - 9b go build 全量编译通过
   - 9c （可选）`go test ./internal/memory/...` 跑通

## 依赖与注意事项

- **SQLite 驱动**：`modernc.org/sqlite` 已存在，新增表可直接复用
- **Git**：`gitstore/version.go` 调用系统 `git` 命令；若系统无 git 则 disable 该功能（非致命）
- **LLM 调用**：compress 的高级模式和 reflect 的高精度抽取需要 provider.Provider，MemoryManager 通过依赖注入获取；缺失时 fallback 到 rule-based
- **字符预算 vs token 预算**：workbuddy 强调"结论而非逐字记忆"，所有层级预算都以字符数为主（沿用 snapshot_manager 的理念）
- **前缀缓存保护**：snapshot 的 frozen 机制必须完整保留，否则会破坏 Anthropic 等 prefix caching
- **向后兼容**：旧的 `Store/Recall/List/Delete` 方法签名必须保留（调用方太多），内部重定向到新三层
- **数据库迁移**：新 MemoryManager 启动时自动执行 schema migration（旧 memories 表 → 新的三层 memories 表，加 level 字段 short/working/long）

## 验证方案

1. **层验证**
   - 短期：push N turn → 召回 N 最近 → 验证 ring buffer 不超容量
   - 工作：写入今日日期 → 验证 YYYY-MM-DD.md 文件生成 + 能检索到
   - 长期：append 超 MEMORY_LIMIT → 验证 compress 正确触发 + MEMORY.md 内容不超预算
   - 元文件：IDENTITY/SOUL/USER 默认模板自动创建

2. **学习周期验证**
   - Reflect：构造一组 conversation → 验证 facts/decisions/preferences 各抽取 >= 1 条
   - Daily：伪造 7 个 daily 文件 → 触发 daily → 验证旧文件归档 + 长期记忆新增 summary 条目
   - Weekly：伪造一批 importance=8 记忆 → 触发 weekly → 验证衰减后 importance 在合理区间

3. **集成验证**
   - `go build ./...` 编译通过（重点 cortex、cmd/magic、agent 包不报错）
   - 启动 chat → 验证 BuildContext 返回包含 IDENTITY/SOUL/USER + 相关 MEMORY 段落
   - 调用 memory_recall → 能命中当日 working 记忆 + 长期记忆

4. **图谱验证**
   - 存两条相关记忆 → trigger 链接 → graph_query 返回邻接节点

## 风险与应对

| 风险 | 处理方式 |
|---|---|
| 工作记忆 YYYY-MM-DD.md 文件跨平台路径问题 | 使用 `filepath.Join` + `os.UserHomeDir()` + `~/.magic/memories/daily/`，不依赖 CWD |
| 系统无 git 导致 gitstore 失败 | `version.go` 启动时探测 git 可执行性；不可用时优雅降级（功能 disable，不影响主流程） |
| LLM provider 注入失败导致 Reflect/Daily 质量差 | Reflect/Daily 都提供 rule-based 抽取 + 压缩作为 fallback，日志 warning 不返回 error |
| 旧数据库数据迁移失败 | Migration 加事务保护；失败时保留旧表并以新表空表启动；记录 migration 日志供人工修复 |
| Snapshot frozen 机制破坏 prefix cache | SnapshotManager 仅数据源替换，frozen/latest 双缓冲和 OnTurnStart/RefreshSnapshot 调用时序**完全不变** |
| 现有调用方 API 签名变更导致编译失败 | store.go 旧方法签名 100% 保留，仅内部委托给 manager；新增方法用新名称 |

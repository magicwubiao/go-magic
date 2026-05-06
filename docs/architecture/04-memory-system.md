# 记忆系统 (Memory System)

## 概述

记忆系统是 Cortex Agent 的核心组件之一，负责存储、检索和管理用户交互过程中产生的信息。它包含两个主要子系统：快照管理和全息记忆检索。

## 双文件存储

### MEMORY.md

存储核心结论和洞见：

- **字符限制**：2,200 字符
- **内容**：任务结论、工作流模式、经验总结
- **更新时机**：每轮对话结束时

### USER.md

存储用户偏好：

- **字符限制**：1,375 字符
- **内容**：语言偏好、风格偏好、习惯偏好
- **更新时机**：识别到新偏好时

## 冻结快照机制

**核心创新**：保护前缀缓存，降低 API 成本。

```
本轮对话进行中:
    MEMORY.md 已更新 ← 写入磁盘
    但系统提示 = 本轮开始时的冻结快照 ← 不加载变更
    ↓
    前缀缓存 HIT 率 100% ← 成本保护

下一轮对话 / 新会话:
    刷新冻结快照
    加载最新的 MEMORY.md
```

### 生命周期

```
┌──────────────────────────────────────────────────────┐
│                      对话轮次                         │
├──────────────────────────────────────────────────────┤
│  Turn N 开始                                         │
│    │                                                 │
│    ├─ OnTurnStart()                                 │
│    │    └─ 冻结当前快照（MEMORY.md + USER.md）        │
│    │                                                 │
│    ├─ 处理用户消息                                   │
│    │                                                 │
│    ├─ 更新 MEMORY.md / USER.md                      │
│    │                                                 │
│    └─ OnTurnEnd()                                   │
│         └─ 写入磁盘（但不加载到系统提示）              │
│                                                      │
│  Turn N+1 开始                                       │
│    │                                                 │
│    ├─ OnTurnStart()                                 │
│    │    └─ 刷新冻结快照（加载最新 MEMORY.md）        │
│    │                                                 │
│    ...                                               │
└──────────────────────────────────────────────────────┘
```

## 数据结构

### Memory

```go
type Memory struct {
    Key        string    // 记忆键
    Value      string    // 记忆值
    Importance int       // 重要性 1-10
    Tags       []string  // 标签
    CreatedAt  time.Time
    UpdatedAt  time.Time
}
```

### Insight

```go
type Insight struct {
    Content   string    // 洞见内容
    Source    string    // 来源
    Category  string    // 分类
    Confidence float64  // 置信度
    CreatedAt time.Time
}
```

### Snapshot

```go
type Snapshot struct {
    MemoryContent  string
    UserPrefs      string
    CreatedAt      time.Time
    Version        int
}
```

## 存储策略

### 重要性分级

| 级别 | 说明 | 保留时间 |
|------|------|----------|
| 9-10 | 关键信息 | 永久 |
| 7-8 | 重要信息 | 30 天 |
| 5-6 | 一般信息 | 7 天 |
| 1-4 | 临时信息 | 1 天 |

### 自动清理

```go
// 定期清理低价值记忆
store.Cleanup(ctx, CleanupPolicy{
    MaxAge:      7 * 24 * time.Hour,
    MinImportance: 5,
    MaxCount:    1000,
})
```

## 使用示例

### 存储记忆

```go
import "github.com/magicwubiao/go-magic/internal/memory"

// 存储一般记忆
err := store.Store(ctx, &Memory{
    Key:        "last_task",
    Value:      "用户执行了数据清洗任务",
    Importance: 6,
    Tags:       []string{"task", "data"},
})

// 存储重要洞见
err = store.StoreInsight(ctx, &Insight{
    Content:   "Python 的 pandas 库适合数据处理",
    Source:    "task_execution",
    Category:  "skill",
    Confidence: 0.9,
})
```

### 检索记忆

```go
// 获取特定记忆
memory, err := store.Get(ctx, "user_preference")

// 搜索记忆
results, err := store.Search(ctx, "数据处理")
for _, r := range results {
    fmt.Printf("记忆: %s\n", r.Value)
}
```

### 快照管理

```go
import "github.com/magicwubiao/go-magic/internal/memory"

// 轮次开始 - 获取冻结快照
mgr.OnTurnStart()
snapshot := mgr.GetFrozenSnapshot()

// 在 LLM 调用中使用快照
response := llm.Call(systemPrompt + snapshot + userMessage)

// 轮次结束 - 更新记忆
store.Store(...)
mgr.OnTurnEnd()
```

## 设计原则

1. **字符限制**：强制提炼精华
2. **冻结快照**：保护前缀缓存
3. **自动清理**：防止记忆膨胀
4. **版本管理**：支持记忆回滚

# FTS 全息记忆检索 (Full-Text Search)

## 概述

FTS（全息记忆检索）系统基于 SQLite FTS5 提供跨会话的全文搜索能力，使 Agent 能够检索历史交互中的相关信息。

## 核心特性

### 全文索引

- 使用 SQLite FTS5 引擎
- BM25 相关性排序
- 支持短语搜索
- 支持布尔查询

### 记忆版本管理

- 支持记忆快照
- 可回滚到任意时间点
- 版本历史记录

### 智能清理

- 基于重要性和时间
- 自动过期低价值记忆
- 防止存储膨胀

## 数据库结构

### 记忆表

```sql
CREATE TABLE memories (
    id TEXT PRIMARY KEY,
    key TEXT NOT NULL,
    value TEXT NOT NULL,
    importance INTEGER DEFAULT 5,
    tags TEXT,
    created_at INTEGER,
    updated_at INTEGER
);

CREATE VIRTUAL TABLE memories_fts USING fts5(
    key,
    value,
    tags,
    content='memories',
    content_rowid='rowid'
);
```

### 版本表

```sql
CREATE TABLE memory_versions (
    id TEXT PRIMARY KEY,
    memory_id TEXT,
    value TEXT,
    version INTEGER,
    created_at INTEGER,
    FOREIGN KEY(memory_id) REFERENCES memories(id)
);
```

## 数据结构

### FTSQuery

```go
type FTSQuery struct {
    Text      string  // 搜索文本
    Limit     int     // 返回数量限制
    Threshold float64 // 最低相关性阈值
    Tags      []string // 标签过滤
    TimeRange *TimeRange // 时间范围
}
```

### FTSResult

```go
type FTSResult struct {
    ID        string
    Key       string
    Content   string
    Score     float64  // BM25 相关性得分
    Importance int
    Tags      []string
    CreatedAt time.Time
}
```

## 搜索算法

### BM25 排序

BM25 是基于词频的排序算法：

```
score(D, Q) = Σ IDF(qi) × (tf(ti, D) × (k1 + 1)) / (tf(ti, D) + k1 × (1 - b + b × |D|/avgdl))

其中:
- tf(ti, D) = 词项 ti 在文档 D 中的词频
- |D| = 文档长度
- avgdl = 平均文档长度
- k1, b = 可调参数 (默认 k1=1.5, b=0.75)
- IDF(qi) = 逆文档频率
```

### 相关性阈值

```go
const (
    ThresholdExcellent = 0.8  // 优秀匹配
    ThresholdGood      = 0.5  // 良好匹配
    ThresholdFair      = 0.3  // 一般匹配
)
```

## 使用示例

### 基础搜索

```go
import "github.com/magicwubiao/go-magic/internal/memory/fts"

results, err := ftsStore.Search(ctx, &fts.FTSQuery{
    Text:  "Python 数据处理",
    Limit: 10,
})

for _, r := range results {
    fmt.Printf("[%.2f] %s\n", r.Score, r.Content)
}
```

### 带过滤的搜索

```go
results, err := ftsStore.Search(ctx, &fts.FTSQuery{
    Text:      "机器学习",
    Limit:     5,
    Threshold: 0.5,
    Tags:      []string{"ai", "python"},
    TimeRange: &fts.TimeRange{
        Start: time.Now().AddDate(0, -1, 0), // 最近一个月
        End:   time.Now(),
    },
})
```

### 短语搜索

```go
results, err := ftsStore.Search(ctx, &fts.FTSQuery{
    Text:  `"深度学习" "卷积神经网络"`,
    Limit: 10,
})
```

### 布尔搜索

```go
results, err := ftsStore.Search(ctx, &fts.FTSQuery{
    Text:  `Python AND (数据 OR 清洗)`,
    Limit: 10,
})
```

## 索引管理

### 重建索引

```go
// 重建 FTS 索引
err = ftsStore.Reindex(ctx)
```

### 优化索引

```go
// 优化查询性能
err = ftsStore.Optimize(ctx)
```

## 版本管理

### 创建快照

```go
snapshotID, err := ftsStore.CreateSnapshot(ctx)
```

### 列出快照

```go
snapshots, err := ftsStore.ListSnapshots(ctx)
for _, s := range snapshots {
    fmt.Printf("ID: %s, Time: %s\n", s.ID, s.CreatedAt)
}
```

### 回滚到快照

```go
err = ftsStore.Rollback(ctx, snapshotID)
```

## 性能优化

### 批量插入

```go
memories := []*Memory{...}
err = ftsStore.StoreBatch(ctx, memories)
```

### 异步索引

```go
// 后台建立索引，不阻塞主线程
ftsStore.IndexAsync(ctx, memory)
```

## 设计原则

1. **相关性优先**：BM25 确保高质量结果
2. **可追溯**：版本管理支持回滚
3. **高效清理**：防止存储膨胀
4. **可扩展**：支持自定义排序

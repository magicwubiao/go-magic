# API 文档

## CortexManager API

核心管理器，提供完整的 Cortex Agent 功能。

### 创建管理器

```go
import "github.com/magicwubiao/go-magic/internal/cortex"

// 创建新管理器
mgr := cortex.NewManager("/data/cortex")

// 启动管理器
mgr.Start()

// 关闭管理器
defer mgr.Stop()
```

### 消息处理

```go
// 处理用户消息
mgr.OnUserMessage("Write a Python ETL pipeline")

// 获取感知结果
fmt.Println("Intent:", mgr.GetIntent())
fmt.Println("Complexity:", mgr.GetTaskComplexity())
fmt.Println("Entities:", mgr.GetEntities())
```

### 意图分类

```go
// 获取分类结果
intent := mgr.GetIntent()
// 可选值: "task", "question", "clarification", "correction", "feedback", "chitchat", "unknown"

// 获取复杂度
complexity := mgr.GetTaskComplexity()
// 可选值: "simple", "medium", "advanced"

// 获取推荐的最大轮次
maxTurns := mgr.GetRecommendedMaxTurns()
// Simple: 8, Medium: 15, Advanced: 25
```

### 执行计划

```go
// 获取执行计划
plan := mgr.GetExecutionPlan()

// 遍历步骤
for _, step := range plan.Steps {
    fmt.Printf("[%d] %s\n", step.ID, step.Description)
    fmt.Printf("    Tools: %v\n", step.Tools)
    fmt.Printf("    DependsOn: %v\n", step.DependsOn)
}

// 检查是否需要子代理
if plan.UseSubAgents {
    fmt.Println("Recommendation: Use sub-agents")
}

// 检查是否需要澄清
if plan.NeedsClarification {
    fmt.Println("Clarification needed:", plan.ClarificationQuestion)
}
```

### 生命周期钩子

```go
// 轮次开始 - 冻结快照
mgr.OnTurnStart()

// ... 执行任务 ...

// 轮次结束 - 更新记忆
mgr.OnTurnEnd()

// 会话结束 - 刷新快照
mgr.OnSessionEnd()
```

### 记忆系统

```go
// 存储信息到记忆
mgr.MemoryStore("user_preference", "Prefers Python over Go")

// 检索记忆
results, _ := mgr.MemoryRecall("search query")
for _, r := range results {
    fmt.Printf("Relevance: %.2f, Content: %s\n", r.Score, r.Content)
}

// 获取冻结快照
snapshot := mgr.GetFrozenSnapshot()
fmt.Println("System Prompt:", snapshot)
```

---

## Perception API

感知层处理用户输入，提取结构和意图。

### 感知结果

```go
import "github.com/magicwubiao/go-magic/internal/perception"

result := perception.Analyze("用户输入", history)

// 访问结果
fmt.Println("Intent:", result.Intent)
fmt.Println("Complexity:", result.Complexity)
fmt.Println("Entities:", result.Entities)
fmt.Println("HasNoise:", result.HasNoise)
```

### 意图分类

```go
type Intent int

const (
    IntentTask          Intent = iota // 任务请求
    IntentQuestion                     // 提问
    IntentClarification                // 澄清请求
    IntentCorrection                   // 纠正
    IntentFeedback                     // 反馈
    IntentChitchat                     // 闲聊
    IntentUnknown                      // 未知
)
```

### 复杂度评估

```go
type Complexity int

const (
    ComplexitySimple    Complexity = iota // 单工具即可完成
    ComplexityMedium                    // 多工具协作
    ComplexityAdvanced                  // 需要子代理
)
```

### 实体提取

```go
type Entity struct {
    Type    string // "language", "file", "tool", "concept"
    Value   string
    Span    [2]int // Start, End positions
}
```

### 噪声检测

```go
type NoiseInfo struct {
    HasNoise   bool
    Types      []string // "incomplete", "ambiguous", "contradictory"
    Suggestions []string
}
```

---

## Cognition API

决策层生成执行计划。

### 决策结果

```go
import "github.com/magicwubiao/go-magic/internal/cognition"

decision := cognition.Plan(perceptionResult)

// 获取决策
fmt.Println("Strategy:", decision.Strategy)
fmt.Println("MaxTurns:", decision.MaxTurns)
fmt.Println("UseSubAgents:", decision.UseSubAgents)
```

### 执行计划

```go
type ExecutionPlan struct {
    Steps         []Step
    UseSubAgents  bool
    UseCheckpoints bool
    NeedsClarification bool
    ClarificationQuestion string
}

type Step struct {
    ID          int
    Description string
    Tools       []string
    DependsOn   []int
    IsCheckpoint bool
}
```

---

## Execution API

执行层运行任务，支持检查点和断点续传。

### 执行管理器

```go
import "github.com/magicwubiao/go-magic/internal/execution"

mgr := execution.NewManager(checkpointDir)
defer mgr.Close()
```

### 运行计划

```go
// 创建执行任务
task := execution.NewTask(plan)

// 运行并获取进度
ch := task.Run()

for progress := range ch {
    fmt.Printf("Progress: %d/%d\n", progress.Current, progress.Total)
    fmt.Printf("Step: %s\n", progress.StepDescription)
    
    if progress.Error != nil {
        fmt.Printf("Error: %v\n", progress.Error)
    }
}
```

### 检查点管理

```go
// 获取所有检查点
checkpoints := mgr.ListCheckpoints()
for _, cp := range checkpoints {
    fmt.Printf("ID: %d, Step: %s, Time: %s\n", 
        cp.ID, cp.StepDescription, cp.Timestamp)
}

// 从检查点恢复
mgr.Restore(checkpointID)

// 清除检查点
mgr.Clear()
```

### 结果校验

```go
type ValidationResult struct {
    Expected   interface{}
    Actual     interface{}
    IsValid    bool
    Diff       []DiffItem
}

type DiffItem struct {
    Path     string
    Expected interface{}
    Actual   interface{}
}
```

---

## Memory API

记忆系统提供存储和检索功能。

### 存储接口

```go
import "github.com/magicwubiao/go-magic/internal/memory"

// 存储记忆
err := store.Store(ctx, &Memory{
    Key:       "user_preference",
    Value:     "Prefers dark mode",
    Importance: 8,
    Tags:      []string{"preference", "ui"},
})

// 存储洞见
err = store.StoreInsight(ctx, "Python GIL limits multi-threading performance")

// 获取记忆
memory, _ := store.Get(ctx, "user_preference")

// 搜索记忆
results, _ := store.Search(ctx, "dark mode")
```

### FTS 检索

```go
import "github.com/magicwubiao/go-magic/internal/memory/fts"

// 全文搜索
results, _ := ftsStore.Search(ctx, &fts.Query{
    Text:     "Python performance",
    Limit:    10,
    Threshold: 0.5,
})

for _, r := range results {
    fmt.Printf("Score: %.2f, Content: %s\n", r.Score, r.Content)
}
```

### 快照管理

```go
import "github.com/magicwubiao/go-magic/internal/memory"

// 创建快照
snapshot, _ := snapshotMgr.CreateSnapshot(ctx)

// 获取冻结快照
frozen := snapshotMgr.GetFrozenSnapshot()
fmt.Println("System Prompt:", frozen)

// 刷新快照
snapshotMgr.RefreshSnapshot(ctx)
```

---

## 错误处理

所有 API 方法返回标准错误：

```go
if err != nil {
    if errors.Is(err, cortex.ErrNotInitialized) {
        // 管理器未初始化
    } else if errors.Is(err, cortex.ErrAlreadyRunning) {
        // 已经在运行
    } else if errors.Is(err, cortex.ErrPlanNotReady) {
        // 执行计划未就绪
    }
}
```

### 常见错误

| 错误 | 说明 |
|------|------|
| `ErrNotInitialized` | 管理器未初始化 |
| `ErrAlreadyRunning` | 已经在运行 |
| `ErrPlanNotReady` | 执行计划未就绪 |
| `ErrCheckpointNotFound` | 检查点不存在 |
| `ErrMemoryLimitExceeded` | 超过记忆限制 |
| `ErrValidationFailed` | 验证失败 |

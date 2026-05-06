# 执行层 (Execution Layer)

## 概述

执行层是 Cortex Agent 的第三层，负责实际执行决策层生成的任务计划。它支持检查点机制和断点续传，确保长时间任务可以可靠完成。

## 核心功能

### 检查点机制

每个执行步骤完成后自动保存状态：

```
Step 1 完成 → 保存 checkpoint_1.json
Step 2 完成 → 保存 checkpoint_2.json
...
```

### 断点续传

支持从中断点恢复执行：

```go
// 检查是否有可恢复的检查点
if manager.HasCheckpoint() {
    // 从最新检查点恢复
    manager.Restore()
}

// 继续执行
manager.Resume()
```

### 结果校验

验证执行结果是否符合预期：

```go
type ValidationResult struct {
    Expected   interface{}
    Actual     interface{}
    IsValid    bool
    Diff       []DiffItem
}
```

### 进度展示

实时向用户展示执行进度：

```go
// 获取进度通道
progressCh := task.Run()

for p := range progressCh {
    fmt.Printf("Progress: %d/%d (%.1f%%)\n", 
        p.Current, p.Total, p.Percent)
    fmt.Printf("Current: %s\n", p.StepDescription)
    
    if p.Error != nil {
        fmt.Printf("Error: %v\n", p.Error)
    }
}
```

## 执行流程

```
┌─────────────────────────────────────┐
│           开始执行                    │
└─────────────────┬───────────────────┘
                  │
                  ▼
┌─────────────────────────────────────┐
│         加载/创建检查点               │
└─────────────────┬───────────────────┘
                  │
                  ▼
┌─────────────────────────────────────┐
│         获取下一个待执行步骤          │
└─────────────────┬───────────────────┘
                  │
        ┌─────────┴─────────┐
        │                   │
        ▼                   ▼
   所有步骤完成        有待执行步骤
        │                   │
        ▼                   ▼
┌───────────────┐   ┌─────────────────────┐
│   任务完成     │   │   执行当前步骤       │
└───────────────┘   └──────────┬──────────┘
                               │
                    ┌──────────┴──────────┐
                    │                     │
                    ▼                     ▼
               执行成功              执行失败
                    │                     │
                    ▼                     ▼
           保存检查点           ┌─────────────────┐
                    │           │ 错误分类         │
                    │           │ 决定是否重试     │
                    │           └─────────────────┘
                    │                   │
                    └─────────┬─────────┘
                              │
                    ┌─────────┴─────────┐
                    │                   │
                    ▼                   ▼
              可重试              不可重试
                    │                   │
                    ▼                   ▼
              重试 N 次          返回错误
```

## 检查点格式

```json
{
  "version": "1.0",
  "task_id": "task_12345",
  "plan_id": "plan_67890",
  "current_step": 3,
  "steps": [
    {
      "id": 1,
      "status": "completed",
      "result": { ... },
      "duration_ms": 1234
    },
    {
      "id": 2,
      "status": "completed",
      "result": { ... },
      "duration_ms": 567
    },
    {
      "id": 3,
      "status": "in_progress",
      "started_at": "2026-05-03T10:00:00Z"
    }
  ],
  "created_at": "2026-05-03T09:00:00Z",
  "updated_at": "2026-05-03T10:00:00Z"
}
```

## 错误处理

### 错误分类

| 类型 | 说明 | 处理方式 |
|------|------|----------|
| `retryable` | 可重试错误 | 自动重试 |
| `user_input` | 需要用户输入 | 暂停并询问 |
| `fatal` | 致命错误 | 终止任务 |

### 重试策略

```go
type RetryPolicy struct {
    MaxRetries   int           // 最大重试次数
    InitialDelay time.Duration // 初始延迟
    MaxDelay     time.Duration // 最大延迟
    Backoff      float64       // 退避系数
}
```

## 使用示例

```go
import "github.com/magicwubiao/go-magic/internal/execution"

// 创建执行管理器
mgr := execution.NewManager("/data/checkpoints")
defer mgr.Close()

// 创建执行任务
task := execution.NewTask(plan)
task.CheckpointDir = "/data/checkpoints"
task.ProgressCallback = func(p Progress) {
    fmt.Printf("Progress: %d/%d\n", p.Current, p.Total)
}

// 执行并处理结果
ch := task.Run()
for p := range ch {
    if p.IsComplete {
        fmt.Println("Task completed!")
        fmt.Printf("Results: %v\n", p.Results)
    }
}
```

## 设计原则

1. **幂等性**：同一检查点可重复执行
2. **可恢复**：任何中断点都可恢复
3. **透明**：用户可见执行状态
4. **高效**：检查点存储开销最小

# 决策层 (Cognition Layer)

## 概述

决策层是 Cortex Agent 的第二层，负责根据感知结果制定执行计划。它将复杂的用户需求分解为可执行的步骤序列。

## 核心功能

### 任务拆解

将复杂任务分解为有序步骤：

```
用户请求: "帮我分析销售数据并生成报告"

拆解结果:
1. 读取销售数据文件
2. 数据清洗和预处理
3. 数据统计分析
4. 生成可视化图表
5. 撰写报告文档
```

### DAG 依赖管理

检测步骤间的依赖关系：

```go
type Step struct {
    ID          int
    Description string
    Tools       []string
    DependsOn   []int  // 依赖的步骤 ID
    IsCheckpoint bool  // 是否需要检查点
}
```

### 动态调整

执行失败时自动重试：

- 检测失败原因
- 添加重试步骤
- 调整后续计划

### Max Turns 自适应

根据复杂度设置轮次限制：

| 复杂度 | 最大轮次 | 说明 |
|--------|----------|------|
| Simple | 8 | 防止简单任务过度消耗 |
| Medium | 15 | 中等任务的标准限制 |
| Advanced | 25 | 复杂任务需要更多探索 |

### 子代理决策

复杂任务自动启用子代理：

```go
if complexity == ComplexityAdvanced {
    decision.UseSubAgents = true
    decision.SubAgentCount = estimateOptimalSubAgents(task)
}
```

### 澄清检测

检测到问题时生成澄清问题：

```go
if perception.HasNoise {
    decision.NeedsClarification = true
    decision.ClarificationQuestion = generateClarificationQuestion(noise)
}
```

## 数据结构

### Decision

```go
type Decision struct {
    Strategy       Strategy
    MaxTurns       int
    UseSubAgents   bool
    SubAgentCount  int
    MemoryQueries  []string
}
```

### ExecutionPlan

```go
type ExecutionPlan struct {
    Steps               []Step
    UseSubAgents        bool
    UseCheckpoints      bool
    NeedsClarification  bool
    ClarificationQuestion string
}
```

### Strategy

```go
type Strategy int

const (
    StrategyDirect Strategy = iota  // 直接执行
    StrategySequential              // 顺序执行
    StrategyParallel                // 并行执行
    StrategyHierarchical            // 分层执行
)
```

## 使用示例

```go
import "github.com/magicwubiao/go-magic/internal/cognition"

// 基于感知结果生成决策
decision := cognition.Plan(perceptionResult)

// 获取执行计划
plan := decision.ExecutionPlan

// 遍历执行步骤
for _, step := range plan.Steps {
    fmt.Printf("Step %d: %s\n", step.ID, step.Description)
    
    // 检查依赖
    if len(step.DependsOn) > 0 {
        fmt.Printf("  Depends on: %v\n", step.DependsOn)
    }
    
    // 检查是否需要检查点
    if step.IsCheckpoint {
        fmt.Println("  [Checkpoint Required]")
    }
}

// 检查是否需要澄清
if plan.NeedsClarification {
    fmt.Println("Clarification needed:", plan.ClarificationQuestion)
}
```

## 设计原则

1. **最优路径**：选择最优的执行路径
2. **最小依赖**：减少步骤间的依赖
3. **可恢复**：失败后可从检查点恢复
4. **透明**：用户可见执行计划

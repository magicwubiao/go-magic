# 技能自进化 (Skill Auto-Evolution)

## 概述

技能自进化系统是 Cortex Agent 的核心创新之一，能够从用户交互中自动识别重复工作流，并生成可复用的技能。

## 工作原理

```
用户交互 → 模式识别 → 技能生成 → 渐进式披露
    ↑                                        │
    └────────────── 使用反馈 ────────────────┘
```

## 模式识别

### 触发条件

系统通过 Nudge 机制定期分析最近交互：

| 触发条件 | 说明 |
|----------|------|
| 每 N 轮对话 | 默认 15 轮触发一次 |
| 新任务边界 | 识别到新任务类型 |
| 重复工作流 | 多次出现相似操作 |

### 模式类型

```go
type PatternType int

const (
    PatternSequential PatternType = iota  // 顺序操作
    PatternParallel                       // 并行操作
    PatternConditional                    // 条件分支
    PatternLoop                           // 循环操作
)
```

## 技能生成

### 生成流程

```
1. 后台复盘分析
   └─ 分析最近 N 轮的工具调用序列
   
2. 识别重复模式
   └─ 找出相似的工作流
   
3. 生成技能草稿
   └─ 创建 Markdown 格式的技能文档
   
4. 用户确认
   └─ 询问用户是否保存
   
5. 技能激活
   └─ 技能进入可用状态
```

### 技能结构

```markdown
# 技能名称

## 描述
一句话描述技能功能

## 使用场景
- 场景 1
- 场景 2

## 步骤
1. 步骤 1
2. 步骤 2

## 示例
```python
# 示例代码
```
```

## 渐进式披露

根据任务复杂度选择披露等级：

| 等级 | 内容 | Token 消耗 | 使用场景 |
|------|------|-----------|----------|
| Level 0 | 名称 + 描述 | ~50 | 简单任务 |
| Level 1 | 完整说明 + 步骤 | ~500 | 中等任务 |
| Level 2 | 文档 + 示例代码 | ~2000+ | 复杂任务 |

### 自动选择策略

```go
func SelectDisclosureLevel(taskComplexity Complexity) DisclosureLevel {
    switch taskComplexity {
    case ComplexitySimple:
        return Level0
    case ComplexityMedium:
        return Level1
    case ComplexityAdvanced:
        return Level2
    }
}
```

## 数据结构

### Skill

```go
type Skill struct {
    ID          string
    Name        string
    Description string
    Triggers    []string  // 触发词
    Steps       []Step
    Examples    []Example
    Level       DisclosureLevel
    UsageCount  int
    CreatedAt   time.Time
    UpdatedAt   time.Time
}
```

### Pattern

```go
type Pattern struct {
    ID          string
    Name        string
    Type        PatternType
    ToolSequence []string
    Frequency   int
    LastSeen    time.Time
}
```

## 使用示例

### 列出技能

```go
skills, _ := skillStore.List(ctx)
for _, s := range skills {
    fmt.Printf("%s: %s\n", s.Name, s.Description)
}
```

### 调用技能

```go
// 通过名称调用
result, _ := skillStore.Invoke(ctx, "data_cleaning", params)

// 自动匹配
result, _ := skillStore.AutoMatch(ctx, "清洗销售数据")
```

### 查看技能详情

```go
skill, _ := skillStore.Get(ctx, "skill_id")

// 获取指定披露等级
content := skill.GetContent(Level1)
fmt.Println(content)
```

## 技能目录

```
skills/
├── data_cleaning.md      # 数据清洗技能
├── report_generation.md  # 报告生成技能
├── code_review.md        # 代码审查技能
└── ...
```

## 后台复盘

### 复盘流程

```go
// 触发后台复盘
reviewer := review.NewBackgroundReviewer()
reviewer.Start(ctx)

// 复盘分析
patterns, _ := reviewer.Analyze(ctx, ReviewConfig{
    ConversationTurns: 15,
    MinFrequency: 2,
})

// 生成技能建议
suggestions := reviewer.GenerateSuggestions(patterns)
```

### 复盘报告

```json
{
  "review_id": "review_123",
  "analyzed_turns": 15,
  "patterns_found": 3,
  "suggestions": [
    {
      "pattern_id": "pattern_456",
      "pattern_name": "数据清洗流程",
      "confidence": 0.85,
      "suggested_skill": "data_cleaning"
    }
  ],
  "created_at": "2026-05-03T10:00:00Z"
}
```

## 学习效果

### 使用 10 次后的变化

| 指标 | 新安装 | 使用 10 次后 |
|------|--------|-------------|
| 任务拆解时间 | 3-5 轮 | 1-2 轮 |
| 工具选择正确率 | ~70% | ~95% |
| 重复工作处理 | 从零开始 | 技能加载，1 轮完成 |

## 设计原则

1. **自动发现**：无需手动定义技能
2. **用户控制**：技能需用户确认
3. **渐进披露**：按需加载，控制成本
4. **持续优化**：使用中迭代改进

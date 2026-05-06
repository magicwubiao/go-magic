# 感知层 (Perception Layer)

## 概述

感知层是 Cortex Agent 的第一层，负责理解和解析用户输入。它将原始的用户消息转换为结构化的感知结果，为后续的决策层提供输入。

## 核心功能

### 意图分类

将用户消息分为 7 种类型：

| 意图 | 描述 | 示例 |
|------|------|------|
| `task` | 任务请求 | "帮我写一个 Python 脚本" |
| `question` | 提问 | "什么是机器学习？" |
| `clarification` | 澄清请求 | "你说的 XX 是什么意思？" |
| `correction` | 纠正 | "不对，应该是 YYY" |
| `feedback` | 反馈 | "这个结果很好" |
| `chitchat` | 闲聊 | "今天天气真不错" |
| `unknown` | 未知 | 无法分类的消息 |

### 复杂度评估

根据任务复杂度分配不同资源：

| 级别 | 轮次限制 | 工具需求 | 子代理 |
|------|----------|----------|--------|
| `simple` | 8 | 1-2 个 | 否 |
| `medium` | 15 | 3-5 个 | 否 |
| `advanced` | 25 | 5+ 个 | 是 |

### 实体提取

识别消息中的关键实体：

- **语言**：编程语言、人类语言
- **文件**：文件路径、文件名
- **工具**：工具名称
- **概念**：关键术语、主题

### 噪声检测

检测消息中的问题：

- **不完整**：信息缺失
- **模糊**：语义不清晰
- **矛盾**：逻辑冲突

## 数据结构

### PerceptionResult

```go
type PerceptionResult struct {
    Intent      Intent          // 意图分类
    Complexity  Complexity       // 复杂度评估
    Entities    []Entity        // 提取的实体
    HasNoise    bool            // 是否有噪声
    NoiseInfo   *NoiseInfo      // 噪声详情
    ContextHint string          // 上下文提示
}
```

### Intent

```go
type Intent int

const (
    IntentTask Intent = iota
    IntentQuestion
    IntentClarification
    IntentCorrection
    IntentFeedback
    IntentChitchat
    IntentUnknown
)
```

### Complexity

```go
type Complexity int

const (
    ComplexitySimple Complexity = iota
    ComplexityMedium
    ComplexityAdvanced
)
```

## 使用示例

```go
import "github.com/magicwubiao/go-magic/internal/perception"

// 分析用户消息
result := perception.Analyze("帮我写一个 Python ETL 脚本", history)

// 处理结果
switch result.Intent {
case perception.IntentTask:
    fmt.Println("这是任务请求")
case perception.IntentQuestion:
    fmt.Println("这是提问")
}

switch result.Complexity {
case perception.ComplexitySimple:
    fmt.Println("简单任务，单工具即可")
case perception.ComplexityMedium:
    fmt.Println("中等复杂度，需要多工具")
case perception.ComplexityAdvanced:
    fmt.Println("复杂任务，建议使用子代理")
}

// 处理噪声
if result.HasNoise {
    fmt.Println("需要澄清：", result.NoiseInfo.Suggestions)
}
```

## 设计原则

1. **快速响应**：感知层应在毫秒级完成
2. **准确分类**：意图分类准确率 > 90%
3. **可扩展**：易于添加新的意图类型
4. **幂等性**：相同输入产生相同输出

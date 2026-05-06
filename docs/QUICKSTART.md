# 快速开始指南

## 安装

### 使用 Go 安装

```bash
go install github.com/magicwubiao/go-magic/cmd/magic@latest
```

### 从源码编译

```bash
git clone https://github.com/magicwubiao/go-magic.git
cd go-magic
make build
```

### Docker 运行

```bash
docker pull magicwubiao/go-magic:latest
docker run -it magicwubiao/go-magic:latest
```

---

## 基础配置

### 首次配置

```bash
# 设置默认 provider
magic config set provider openai

# 设置 API key
magic config set providers.openai.api_key YOUR_API_KEY

# 查看当前配置
magic config show
```

### 配置文件

配置文件位于 `~/.magic/config.json`：

```json
{
  "provider": "openai",
  "model": "gpt-4",
  "providers": {
    "openai": {
      "api_key": "your-api-key",
      "base_url": "https://api.openai.com/v1"
    },
    "deepseek": {
      "api_key": "your-deepseek-key",
      "base_url": "https://api.deepseek.com"
    }
  },
  "agent": {
    "max_turns": 10,
    "max_context_length": 200000,
    "compression_enabled": true,
    "compression_ratio": 0.7
  },
  "cortex": {
    "enabled": true,
    "frozen_snapshot": true,
    "nudge_threshold": 15
  }
}
```

---

## 基础使用

### 交互式对话

```bash
# 启动交互式对话
magic chat

# 指定模型
magic chat --model gpt-4

# 指定 provider
magic chat --provider deepseek
```

### 单次请求

```bash
magic ask "What is the capital of France?"
```

---

## Cortex 模式

Cortex 模式启用完整的三层认知架构和六大自进化系统。

### 启用 Cortex

在配置文件中启用：

```json
{
  "cortex": {
    "enabled": true,
    "frozen_snapshot": true,
    "nudge_threshold": 15,
    "memory_limit": 2200,
    "user_preference_limit": 1375
  }
}
```

### 使用 Cortex API

```go
import "github.com/magicwubiao/go-magic/internal/cortex"

// 创建管理器
mgr := cortex.NewManager("/data/cortex")
mgr.Start()

// 用户消息进入
mgr.OnUserMessage("Write a Python ETL pipeline")

// 获取感知结果
fmt.Println("Intent:", mgr.GetIntent())
fmt.Println("Complexity:", mgr.GetTaskComplexity())

// 获取执行计划
plan := mgr.GetExecutionPlan()
for _, step := range plan.Steps {
    fmt.Printf("[%d] %s\n", step.ID, step.Description)
}
```

---

## 工具系统

### 启用内置工具

```json
{
  "tools": {
    "enabled": [
      "web_search",
      "read_file",
      "write_file",
      "execute_command",
      "python_execute"
    ]
  }
}
```

### MCP 服务器

连接外部 MCP 服务器：

```bash
# 连接文件系统 MCP 服务器
magic mcp connect filesystem npx -y @modelcontextprotocol/server-filesystem /tmp

# 列出已连接服务器
magic mcp list

# 检查服务器健康状态
magic mcp health

# 断开连接
magic mcp disconnect filesystem
```

配置文件：

```json
{
  "mcp": {
    "servers": {
      "filesystem": {
        "command": "npx",
        "args": ["-y", "@modelcontextprotocol/server-filesystem", "/tmp"],
        "transport": "stdio"
      }
    }
  }
}
```

---

## 消息网关

### 启动网关

```bash
magic gateway start
```

### 支持的平台

| 平台 | 配置字段 |
|------|----------|
| Telegram | `gateway.platforms.telegram` |
| Discord | `gateway.platforms.discord` |
| 飞书 | `gateway.platforms.feishu` |
| 企业微信 | `gateway.platforms.wecom` |
| QQ | `gateway.platforms.qq` |
| DingTalk | `gateway.platforms.dingtalk` |

### Telegram 配置示例

```json
{
  "gateway": {
    "enabled": true,
    "port": 8080,
    "platforms": {
      "telegram": {
        "enabled": true,
        "bot_token": "YOUR_BOT_TOKEN"
      }
    }
  }
}
```

---

## 语音模式

### 启用语音

```bash
# 启动语音监听（按键说话）
magic voice listen

# 文字转语音
magic voice speak "Hello, I am magic Agent"

# 测试语音配置
magic voice test
```

配置：

```json
{
  "voice": {
    "stt_provider": "whisper",
    "tts_provider": "openai",
    "tts_voice": "alloy",
    "push_to_talk_key": "space"
  }
}
```

---

## 隐私保护

### PII 检测与脱敏

```bash
# 脱敏文本
magic privacy redact "My phone is 13812345678"

# 检测 PII
magic privacy detect "Email: test@example.com, IP: 192.168.1.1"

# 查看审计日志
magic privacy audit

# 查看统计
magic privacy stats
```

配置：

```json
{
  "privacy": {
    "enabled": true,
    "redact_phone": true,
    "redact_email": true,
    "redact_id_card": true,
    "redact_bank_card": true,
    "redact_ip": true
  }
}
```

---

## 下一步

- 阅读 [架构文档](./ARCHITECTURE.md) 了解系统设计
- 阅读 [API 文档](./API.md) 了解编程接口
- 阅读 [测试指南](./TESTING.md) 了解测试方法

# magic setup

Interactive setup wizard that configures everything at once.

交互式配置向导，一次性完成所有配置。

## Usage / 用法

```bash
magic setup
```

## First Run / 首次运行

When you run `magic chat` for the first time (no `~/.magic/config.json` exists), the setup wizard is automatically launched:

首次运行 `magic chat` 时（`~/.magic/config.json` 不存在），会自动启动配置向导：

```
Welcome to magic! It looks like this is your first run.
Let's set things up...

╔════════════════════════════════════════╗
║       magic Agent Setup Wizard         ║
╚════════════════════════════════════════╝
```

After setup completes, the chat session starts automatically with your configuration.

配置完成后，聊天会话会自动使用你的配置启动。

You can also run `magic setup` manually at any time to reconfigure.

随时可手动运行 `magic setup` 重新配置。

## Setup Steps / 配置步骤

### 1. LLM Provider / LLM 提供商

Select the LLM provider to use.

选择要使用的 LLM 提供商。

| # | Provider | Default Model | Default Base URL |
|---|----------|---------------|------------------|
| 1 | DeepSeek | deepseek-chat | https://api.deepseek.com/v1 |
| 2 | Anthropic (Claude) | claude-3-5-sonnet-20241022 | https://api.anthropic.com/v1 |
| 3 | OpenAI (GPT-4, GPT-3.5) | gpt-4o | https://api.openai.com/v1 |
| 4 | Kimi (Moonshot) | moonshot-v1-8k | https://api.moonshot.cn/v1 |
| 5 | Zhipu (GLM) | glm-4 | https://open.bigmodel.cn/api/paas/v4 |
| 6 | Huoshan (Volcano Engine) | ep-20250105-xxxxx | https://volcengine.com/api/v1 |
| 7 | MiniMax | abab6-chat | https://api.minimax.chat/v1 |
| 8 | Dashscope (Qwen) | qwen-turbo | https://dashscope.aliyuncs.com/api/v1 |
| 9 | Ollama (Local) | llama3.2 | http://localhost:11434/v1 |
| 10 | vLLM (Local) | - | http://localhost:8000/v1 |
| 11 | OpenRouter | openrouter/anthropic/claude-3.5-sonnet | https://openrouter.ai/api/v1 |
| 12 | Other (custom) | - | - |

**Input**: Type the number (1-12) and press Enter, or press Enter directly to keep the current selection.

**输入**：输入数字（1-12）后按 Enter，或直接按 Enter 保留当前选项。

### 2. API Key / API 密钥

Enter the API key for the selected provider. Existing keys are shown masked (last 4 characters only).

输入所选提供商的 API 密钥。已有密钥会脱敏显示（仅显示最后 4 位）。

- For Ollama, vLLM, and custom providers, the API key is optional.
- 对于 Ollama、vLLM 和自定义提供商，API 密钥为可选。

### 3. Custom Provider Fields / 自定义提供商字段 (only for "Other")

When selecting "Other (custom)", you need to additionally provide:

选择"Other (custom)"时，需额外提供：

- **API Base URL** — The API endpoint URL / API 端点地址
- **Model name** — The model identifier / 模型标识符

### 4. Model Selection / 模型选择

Select a model for the chosen provider. Available models vary by provider:

为所选提供商选择模型。可用模型因提供商而异：

| Provider | Available Models |
|----------|-----------------|
| DeepSeek | deepseek-chat, deepseek-reasoner, deepseek-v3.1, deepseek-coder |
| Anthropic | claude-sonnet-4-20250514, claude-opus-4-20250514, claude-3-5-sonnet-20241022, claude-3-5-haiku-20241022, claude-3-opus-20240229 |
| OpenAI | gpt-4.1, gpt-4.1-mini, gpt-4.1-nano, gpt-4o, gpt-4o-mini, o3-mini, gpt-4-turbo, gpt-4 |
| Kimi | moonshot-v1-128k, moonshot-v1-32k, moonshot-v1-8k, kimi-k2-instruct |
| Zhipu | glm-4, glm-4-flash, glm-4.6, glm-4.7, glm-4v |
| Huoshan | ep-xxxxxxxx (endpoint ID) |
| MiniMax | MiniMax-M2, MiniMax-M2.1, MiniMax-M2.5 |
| Dashscope | qwen3-turbo, qwen3-plus, qwen3-max, qwen3-nano, qwq-32b, qwen-turbo |
| Ollama | llama3.3, qwen3, qwen2.5, codellama, mistral |
| OpenRouter | openrouter/anthropic/claude-sonnet-4, openrouter/google/gemini-2.0-flash, openrouter/deepseek/deepseek-chat |
| vLLM | (user input, depends on local server) |

**Input**: Type the number to select from the list, or type a custom model name directly. Press Enter to confirm the default.

**输入**：输入数字从列表选择，或直接输入自定义模型名。按 Enter 确认默认值。

### 5. Profile Name / 配置文件名称

Set a profile name for this configuration (default: "default").

为此配置设置配置文件名称（默认："default"）。

### 6. Cortex AI Enhancement / Cortex AI 增强

Enable or disable Cortex AI enhancement for memory and context awareness.

启用或禁用 Cortex AI 增强，用于记忆和上下文感知。

- **y** — Enable / 启用
- **N** — Disable (default) / 禁用（默认）

### 7. Messaging Gateway / 消息网关

Enable or disable the messaging gateway for multi-platform bot support.

启用或禁用消息网关，支持多平台机器人。

- **y** — Enable / 启用
- **N** — Disable (default) / 禁用（默认）

If enabled, you can configure messaging platforms:

启用后，可配置消息平台：

| # | Platform |
|---|----------|
| 1 | Telegram |
| 2 | Discord |
| 3 | WeCom (Enterprise WeChat) |
| 4 | Feishu (Lark) |
| 5 | DingTalk |
| 6 | QQ (Bot/Channel) |
| 7 | WeChat (Official Account/Mini Program) |
| 8 | WeChat-iLink (Personal WeChat via iLink) |
| 9 | Slack |
| 10 | WhatsApp |
| 11 | LINE |
| 12 | Matrix |
| 0 | Done |

Press Enter (default 0) to skip platform configuration.

按 Enter（默认 0）跳过平台配置。

## Configuration File / 配置文件

The configuration is saved to `~/.magic/config.json`.

配置保存在 `~/.magic/config.json`。

Example configuration / 示例配置：

```json
{
  "profile": "default",
  "magic_home": "~/.magic",
  "provider": "deepseek",
  "model": "deepseek-chat",
  "cortex_enabled": true,
  "providers": {
    "deepseek": {
      "api_key": "sk-xxxx",
      "base_url": "https://api.deepseek.com/v1",
      "model": "deepseek-chat"
    }
  },
  "tools": {
    "enabled": ["all"]
  },
  "gateway": {
    "enabled": false,
    "platforms": {}
  }
}
```

## After Setup / 配置完成后

Start chatting with:

开始对话：

```bash
magic chat
```

To reconfigure at any time, run `magic setup` again.

随时可重新运行 `magic setup` 进行重新配置。

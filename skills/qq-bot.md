---
name: qq-bot
description: "QQ机器人开发指南 - 使用QQ官方Bot接口开发QQ机器人"
version: 1.0.0
author: magic
license: MIT
metadata:
  hermes:
    tags: [qq, bot, robot, chat, api]
    category: software-development
---

# QQ机器人开发指南

QQ机器人官方开放平台，支持在 **QQ频道**、**QQ群**、**消息列表单聊**等场景下开发和部署机器人。

## 官方资源

| 资源 | 地址 |
|------|------|
| 开放平台官网 | https://q.qq.com / https://bot.q.qq.com |
| 官方文档 | https://bot.q.qq.com/wiki/ |
| 管理端 | 开发者平台 |

## 接入流程

### 1. 注册开发者
- **企业主体**：需要营业执照、对公账户
- **个人主体**：需要身份证、手机号实名认证

### 2. 创建机器人
- 在开发者平台创建应用，获取 **AppID**、**Token**、**AppSecret**

### 3. 配置沙箱环境
- 设置沙箱频道（≤20人）、沙箱群（≤20人）
- 将机器人添加到沙箱环境进行开发测试

### 4. 开发
- 使用 WebSocket 或 Webhook 接收事件
- 调用 OpenAPI 发送消息、操作频道等

### 5. 发布上线
- 配置指令/服务/快捷菜单
- 填写自测报告
- 提交审核 → 审核通过 → 手动上线

## 开发基础

### 接口地址

| 环境 | 地址 |
|------|------|
| 正式环境 | `https://api.sgroup.qq.com/` |
| 沙箱环境 | `https://sandbox.api.sgroup.qq.com/` |
| 获取凭证 | `https://bots.qq.com/app/getAppAccessToken` |

### 认证方式
使用 **Bot Token**（Bot AppID + Bot Token）进行 API 调用鉴权。

```
Authorization: Bot {AppID}.{Token}
```

### 事件接收方式
1. **WebSocket**：通过 Gateway API 获取 WebSocket 连接地址，建立长连接接收事件
2. **Webhook**：配置回调 URL，平台推送事件到该地址

## 支持场景

| 场景 | 企业 | 个人 |
|------|:----:|:----:|
| QQ频道 | ✅ | ✅ |
| QQ群 | ✅ | ✅ |
| 消息列表单聊 | ✅ | ✅ |

## 常用 API 功能

- 发送频道消息
- 发送群消息
- 发送私信消息
- 获取频道/群成员信息
- 消息回复与处理
- 指令面板配置
- 服务跳转小程序

## 开发 SDK（社区）

### Python
```python
# 常用社区库
# pip install qq-bot       # 轻量封装
# pip install qq-botpy     # 官方Python SDK

from qq_bot import QQBot

bot = QQBot(appid="YOUR_APPID", token="YOUR_TOKEN")

@bot.on_message()
async def handle_message(event):
    await bot.reply(event, "Hello from QQ Bot!")

bot.run()
```

### Node.js
```javascript
// npm install qq-guild-bot
const { createClient } = require('qq-guild-bot');

const client = createClient({
  appID: "YOUR_APPID",
  token: "YOUR_TOKEN",
  intents: ['PUBLIC_GUILD_MESSAGES']
});

client.on('READY', () => console.log('Bot ready!'));
client.on('GUILD_MESSAGES', (data) => {
  client.api.message.post(data.msg.channel_id, {
    content: 'Hello!'
  });
});
```

### Go
```go
// go get github.com/tencent-connect/botgo
import "github.com/tencent-connect/botgo"

func main() {
    token := botgo.BotToken(appID, token)
    api := botgo.NewOpenAPI(token).WithTimeout(5 * time.Second)
    // ...
}
```

## 快速开始示例

### 1. 获取 WebSocket 连接
```bash
curl -X GET https://api.sgroup.qq.com/gateway \
  -H "Authorization: Bot {AppID}.{Token}"
```

### 2. 发送消息（频道）
```bash
curl -X POST https://api.sgroup.qq.com/channels/{channel_id}/messages \
  -H "Authorization: Bot {AppID}.{Token}" \
  -H "Content-Type: application/json" \
  -d '{"content": "Hello QQ!"}'
```

### 3. 发送消息（群）
```bash
curl -X POST https://api.sgroup.qq.com/groups/{group_openid}/messages \
  -H "Authorization: Bot {AppID}.{Token}" \
  -H "Content-Type: application/json" \
  -d '{"content": "群消息测试"}'
```

## 注意事项

1. **IP白名单**：新增机器人默认开启，需配置公网IP
2. **消息URL白名单**：消息中的链接域名需提前备案报备
3. **指令配置上限**：服务/指令各24个，快捷菜单各12个
4. **审核要求**：首次上线需提交审核，修改基本信息每月限5次
5. **沙箱限制**：沙箱频道/群成员不超过20人

## 重要链接

- 开放平台: https://q.qq.com
- 官方文档: https://bot.q.qq.com/wiki/
- 开发者社区: QQ频道官方开发者社群
- 联系邮箱: qqbot_developer@tencent.com

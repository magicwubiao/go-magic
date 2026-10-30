---
name: dingtalk
description: 通过钉钉群自定义机器人 Webhook 在钉钉群发送文本或 Markdown 通知。当用户要"发钉钉消息 / 群通知 / @钉钉 某件事"时使用。
version: 1.0.0
tags:
  - dingtalk
  - notification
---

# 钉钉群通知技能

本技能配套 `ap_dingtalk_cli_dingtalk_send_text` 与 `ap_dingtalk_cli_dingtalk_send_markdown` 两个工具，通过钉钉群"自定义机器人"的 Webhook 发消息。

## 授权

Webhook 地址属于凭据，存在连接器 env 的 `DINGTALK_WEBHOOK` 里，由用户在插件页"授权"表单中填写。用户还没填时，工具会报 `env "DINGTALK_WEBHOOK" is empty`——此时引导用户：

1. 在钉钉群里 → 群设置 → 机器人 → 添加"自定义"机器人；
2. 安全设置选**"自定义关键词"**（推荐）或 IP 白名单——本技能不支持加签模式（HMAC 签名 curl 算不了）；
3. 把拿到的 Webhook 地址（形如 `https://oapi.dingtalk.com/robot/send?access_token=...`）填进授权表单。

## 用法

消息体（payload 参数）**必须是完整的 JSON 请求体**，经 stdin 传给 curl，不要试图拼 URL：

```json
{"msgtype": "text", "text": {"content": "今晚十点发布新版本"}}
```

```json
{"msgtype": "markdown", "markdown": {"title": "发布计划", "text": "**周四** 上线\n\n- 列表项"}}
```

- `content`/`text` 里的换行写 `\n`；正文含引号时记得 JSON 转义。
- markdown 的 `title` 会出现在群通知栏，要能概括正文。
- 被"自定义关键词"拦下的消息返回 `errcode:310000`——提醒用户把关键词加进机器人安全设置，或让消息正文包含该关键词。

## 安全边界

1. **发送前必须把要发的目标群（能从上下文推断时）、标题和正文复述给用户**，得到确认再调用——工具走审批链路是最后一道防线，不是替代确认的借口。
2. 不要循环/批量发消息刷群；同一件事发一条说清楚。
3. Webhook 是这个群唯一的入口凭证，**绝不把 Webhook 地址写进消息内容、日志或回复里**。
4. 工具只发消息，不读群历史；用户要"查看群消息"时如实告知超出本技能能力。

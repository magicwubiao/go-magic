# 05-media-provider：给内置生图工具"换后端"

**演示点**：`plugin.json` 的 `extensions` 扩展命名空间——插件不注册新工具名，而是往内置 `image_gen` 工具的后端列表里加一个 provider。这是规范之外的可选扩展（其他客户端会忽略）。

## 目录结构

```
05-media-provider/
  plugin.json                      # extensions 里声明 provider 绑定
  mcp.json                         # provider 跟着 MCP 条目走：条目连上它才可用
  server/stub_image_server.py      # 假的"生图服务"：按 prompt 哈希画渐变 PNG，返回 b64
```

## plugin.json 怎么读

```json
"extensions": {
  "io.github.magicwubiao.media-provider": {
    "image": [{ "name": "stub", "server": "stubgen", "tool": "generate_image" }]
  }
}
```

- `name`：provider 名字，用户在 `config.json` 写 `"image_gen": {"provider": "stub"}`
  （或环境变量 `IMAGE_GEN_PROVIDER=stub`，或网页「设置 → 媒体」里切换）。
- `server`：`mcp.json` 里的条目名；条目断开或插件禁用时，provider 自动摘除，
  `image_gen` 工具直接不再出现该后端。
- `tool`：该 server 上负责生成的工具名。
- 语音（`speech`）用 `tts_tool` / `asr_tool` 分别绑定，可以只声明其中一个。

## 产物契约（工具返回值）

内核拿到地址或字节后**当场下载落盘**，模型只看到本地文件路径：

| 类型 | 可接受的返回 |
|---|---|
| 图片 | `{"url": "..."}` / `{"urls": [...]}` / `{"images": [{"url"\|"b64": ..., "format": "png"}]}` / `{"b64": "..."}` / 纯文本 URL / data URI |
| 视频 | `{"url": "..."}` / `{"video_url": "..."}` / `{"urls": [...]}` / 纯文本 URL |
| 语音合成 | `{"url"\|"audio_url"}` / `{"b64"\|"audio_b64"}` / `{"audio": "data:audio/mpeg;base64,..."}` / 纯文本 URL 或 data URI；可带 `format` / `mime` |
| 语音识别 | `{"text"\|"transcript": "..."}` / 纯文本 |

空字节 + 成功状态会被当成失败——不要让用户拿到一段放不出来的文件。

## 安装与验证

```bash
magic plugin install ./examples/plugins/05-media-provider   # 需要 python
```

1. 插件页确认 `stubgen` 条目已连接；
2. 「设置 → 媒体」把生图 provider 切到 `stub`（或在 config.json 写
   `"image_gen": {"provider": "stub"}`）;
3. 聊天里说 "画一张渐变图"，`image_gen` 的返回会是一张按 prompt 哈希上色的 64x64 PNG。

## 接真实厂商

把 `stub_image_server.py` 的 `generate_image` 换成对厂商 API 的调用即可，返回值形状不变。
注意 `generate_image` 的参数由内核生成（prompt、尺寸等），你的工具 schema 应当宽松接收。

## 为什么要"换后端"而不是注册新工具

`kling_generate` 这类新工具名会让模型在多个生图工具之间挑错；provider 只扩充
"后端列表"，工具名、参数 schema、审批、落盘全部留在内核。声明写错也不会让插件挂掉——
不合格的绑定只记一条 warning，插件的技能与 MCP 照常加载。

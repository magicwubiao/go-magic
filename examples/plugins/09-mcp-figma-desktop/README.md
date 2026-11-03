# 09 · Figma 桌面版 MCP

Figma 桌面版自带的 MCP server：**免 token**，走本机 `127.0.0.1:3845`，直接用当前登录的账号读 Dev Mode 里的选中内容。

> **为什么不在内置目录里？**
> `127.0.0.1:3845` 只有在 **Figma 客户端和 go-magic 同一台机器**、且客户端开着 Dev Mode 时才有东西在听。go-magic 经常部署在服务器上，内置成目录条目就是一条点了必然被拒的条目。
>
> 需要"没有桌面版也能读 Figma"的场景用内置的 `Figma (API token)`（个人访问令牌，纯远程）。

## 前置条件

1. 装 **Figma 桌面版**（网页版不行，MCP server 在客户端里）；
2. 打开目标文件，切到 **Dev Mode**，选中一个 frame / 组件；
3. 确认客户端里的本地 MCP server 已启用（Figma 菜单 → Preferences → 勾选 Enable local MCP server）；
4. 装本插件并连接。

```bash
magic plugin install ./examples/plugins/09-mcp-figma-desktop
```

## 失败长什么样

| 现象 | 原因 |
|---|---|
| 连接被拒绝（connection refused） | 客户端没开，或没启用 local MCP server |
| 连上了但工具列表为空 | 没有选中任何 frame，或没进 Dev Mode |
| 工具返回的设计稿是空的 | 选中的不是 frame（选中了图层里的文字/图片节点） |

## 和 `Figma (API token)` 的区别

| | 桌面版（本示例） | API token（内置） |
|---|---|---|
| 需要客户端 | 是 | 否，纯远程 |
| 凭据 | 无（用客户端登录态） | 个人访问令牌 |
| 能读什么 | 当前打开文件里的选中内容 + Code Connect | 任何有权限的文件，按 URL 读 |
| 适合 | 本地边设计边写代码 | 服务器 / CI 里批量读设计数据 |

# Youdao Note Plugin for go-magic

通过 agent 直接操作有道云笔记。

## 使用场景

```
你: 帮我查一下有道云笔记里有没有关于Go的笔记
Agent: [调用 plugin_youdao-note_search，keyword="Go"]
Agent: 找到2条相关笔记：
  1. Go语言学习笔记
  2. Go并发编程调研

你: 读一下第一条
Agent: [调用 plugin_youdao-note_read，note_id="n004"]
Agent: 笔记内容：...

你: 新建一个笔记，标题"今日工作"，内容"完成了插件系统开发"
Agent: [调用 plugin_youdao-note_create，title="今日工作"，content="..."]
Agent: 笔记已创建 ✓
```

## 安装

```bash
# 1. 复制到插件目录
cp -r plugins/youdao-note/ ~/.magic/plugins/youdao-note/

# 2. 配置 Token
export YOUDAO_TOKEN="your-token-here"

# 3. 加载插件
magic plugin load ~/.magic/plugins/youdao-note
```

## 命令

| 命令 | 说明 | Agent调用名 |
|------|------|------------|
| list | 列出笔记本和最近笔记 | `plugin_youdao-note_list` |
| search | 搜索笔记 | `plugin_youdao-note_search` |
| read | 读取笔记内容 | `plugin_youdao-note_read` |
| create | 创建笔记 | `plugin_youdao-note_create` |
| delete | 删除笔记 | `plugin_youdao-note_delete` |

## 配置

```json
{
  "token": "your-api-token",
  "default_notebook": "default"
}
```

## 当前状态

⚠️ **Stub 模式** — 当前返回模拟数据。需要替换为真实的有道云笔记 API 调用。

替换方法：编辑 `youdao.sh`，将 `cmd_*` 函数里的 `cat <<EOF` 替换为 `curl` 调用即可。

## 开发自己的 CLI 插件

以此插件为模板：
1. 复制 `plugins/youdao-note/` 目录
2. 修改 `manifest.json`：id、name、commands
3. 修改 `youdao.sh`：实现真实的 CLI/API 调用
4. 加载后 agent 自动获得新工具

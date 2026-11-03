#!/usr/bin/env python3
"""最小可用的 MCP stdio server（只依赖 Python 标准库）。

用它当模板可以直接改出"本地文件 + 几个工具"这类插件：没有第三方依赖、
没有端口、没有常驻进程，宿主拉起子进程后按行收发 JSON-RPC。

协议要点：
  - 一行一个 JSON 对象，UTF-8，绝不换行排版；响应必须 flush。
  - stdout 只允许出现协议帧；任何调试输出都走 stderr，否则帧会错位。
  - initialize 必须回 serverInfo 与 capabilities.tools；版本号照抄请求即可。
  - notifications/*（含 initialized）不回复。
"""

import json
import os
import sys
import tempfile
from datetime import datetime, timezone

# 数据文件位置：宿主把 PLUGIN_DATA 展开后通过 env 传进来；
# 单独手跑时退回脚本同目录，便于调试。
DB_PATH = os.environ.get("NOTES_DB") or os.path.join(
    os.path.dirname(os.path.abspath(__file__)), "notes.json"
)

TOOLS = [
    {
        "name": "notes_add",
        "description": "新增一条笔记，返回笔记 id 与落盘路径。",
        "inputSchema": {
            "type": "object",
            "properties": {
                "text": {"type": "string", "description": "笔记正文（必填）"},
                "tags": {
                    "type": "array",
                    "items": {"type": "string"},
                    "description": "可选标签，用于后续筛选",
                },
            },
            "required": ["text"],
        },
    },
    {
        "name": "notes_search",
        "description": "按关键词在笔记正文与标签里做不区分大小写的子串搜索，返回命中的笔记。",
        "inputSchema": {
            "type": "object",
            "properties": {
                "query": {"type": "string", "description": "搜索词"},
                "limit": {
                    "type": "integer",
                    "description": "最多返回条数，默认 20",
                    "default": 20,
                },
            },
            "required": ["query"],
        },
    },
    {
        "name": "notes_list",
        "description": "按时间倒序列出最近的笔记。",
        "inputSchema": {
            "type": "object",
            "properties": {
                "limit": {
                    "type": "integer",
                    "description": "最多返回条数，默认 20",
                    "default": 20,
                }
            },
        },
    },
]


def load_notes():
    """读数据文件。文件不存在 = 空列表；内容损坏时保留原文件并报错，不静默清空。"""
    try:
        with open(DB_PATH, "r", encoding="utf-8") as f:
            data = json.load(f)
    except FileNotFoundError:
        return []
    except json.JSONDecodeError as exc:
        raise RuntimeError(
            "notes db is corrupted (%s): %s" % (DB_PATH, exc)
        ) from exc
    return data if isinstance(data, list) else []


def save_notes(notes):
    """原子写：先写同目录临时文件再 os.replace，避免中途失败留下半截 JSON。"""
    directory = os.path.dirname(DB_PATH) or "."
    os.makedirs(directory, exist_ok=True)
    fd, tmp = tempfile.mkstemp(dir=directory, prefix=".notes-", suffix=".json")
    try:
        with os.fdopen(fd, "w", encoding="utf-8") as f:
            json.dump(notes, f, ensure_ascii=False, indent=2)
            f.flush()
            os.fsync(f.fileno())
        os.replace(tmp, DB_PATH)
    except BaseException:
        try:
            os.unlink(tmp)
        except OSError:
            pass
        raise


def text_result(text, is_error=False):
    return {
        "content": [{"type": "text", "text": text}],
        "isError": is_error,
    }


def tool_add(args):
    text = (args.get("text") or "").strip()
    if not text:
        return text_result("text is required", is_error=True)
    notes = load_notes()
    now = datetime.now(timezone.utc).isoformat(timespec="seconds")
    note = {
        "id": len(notes) + 1,
        "text": text,
        "tags": [t for t in (args.get("tags") or []) if isinstance(t, str)],
        "created_at": now,
    }
    notes.append(note)
    save_notes(notes)
    return text_result(
        "saved note #%d at %s: %s" % (note["id"], DB_PATH, text)
    )


def tool_search(args):
    query = (args.get("query") or "").strip().lower()
    if not query:
        return text_result("query is required", is_error=True)
    limit = max(1, int(args.get("limit") or 20))
    hits = []
    for note in reversed(load_notes()):
        haystack = " ".join([note.get("text", "")] + list(note.get("tags") or []))
        if query in haystack.lower():
            hits.append(note)
        if len(hits) >= limit:
            break
    if not hits:
        return text_result("no notes matched %r" % query)
    lines = [
        "#%s [%s] %s" % (n.get("id"), n.get("created_at", "?"), n.get("text", ""))
        for n in hits
    ]
    return text_result("\n".join(lines))


def tool_list(args):
    limit = max(1, int(args.get("limit") or 20))
    notes = load_notes()[-limit:]
    if not notes:
        return text_result("no notes yet")
    lines = [
        "#%s [%s] %s" % (n.get("id"), n.get("created_at", "?"), n.get("text", ""))
        for n in reversed(notes)
    ]
    return text_result("\n".join(lines))


HANDLERS = {
    "notes_add": tool_add,
    "notes_search": tool_search,
    "notes_list": tool_list,
}


def handle(req):
    """返回响应对象；返回 None 表示这是通知，不需要回复。"""
    method = req.get("method")
    req_id = req.get("id")

    if method is None:
        return None  # 客户端发来的响应（本 server 不主动请求，忽略）

    if method.startswith("notifications/"):
        return None

    if method == "initialize":
        params = req.get("params") or {}
        return {
            "jsonrpc": "2.0",
            "id": req_id,
            "result": {
                # 版本协商：照抄客户端提议的版本，最省事也最兼容握手代客户端。
                "protocolVersion": params.get("protocolVersion", "2025-06-18"),
                "capabilities": {"tools": {"listChanged": False}},
                "serverInfo": {"name": "notes-mcp-demo", "version": "1.0.0"},
            },
        }

    if method == "ping":
        return {"jsonrpc": "2.0", "id": req_id, "result": {}}

    if method == "tools/list":
        return {"jsonrpc": "2.0", "id": req_id, "result": {"tools": TOOLS}}

    if method == "tools/call":
        params = req.get("params") or {}
        name = params.get("name")
        args = params.get("arguments") or {}
        handler = HANDLERS.get(name)
        if handler is None:
            return {
                "jsonrpc": "2.0",
                "id": req_id,
                "error": {"code": -32602, "message": "unknown tool: %s" % name},
            }
        try:
            return {"jsonrpc": "2.0", "id": req_id, "result": handler(args)}
        except Exception as exc:  # 工具错误回 isError，而不是 JSON-RPC error
            return {
                "jsonrpc": "2.0",
                "id": req_id,
                "result": text_result("%s: %s" % (type(exc).__name__, exc), is_error=True),
            }

    return {
        "jsonrpc": "2.0",
        "id": req_id,
        "error": {"code": -32601, "message": "method not found: %s" % method},
    }


def main():
    # 行缓冲：每帧立刻发出去，否则宿主会一直等。
    for raw in sys.stdin:
        line = raw.strip()
        if not line:
            continue
        try:
            req = json.loads(line)
        except json.JSONDecodeError as exc:
            print("skip malformed frame: %s" % exc, file=sys.stderr)
            continue
        resp = handle(req)
        if resp is None:
            continue
        sys.stdout.write(json.dumps(resp, ensure_ascii=False) + "\n")
        sys.stdout.flush()


if __name__ == "__main__":
    try:
        main()
    except KeyboardInterrupt:
        pass

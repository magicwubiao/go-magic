#!/usr/bin/env python3
"""媒体后端示例：一个假的"生图服务"（MCP stdio，只依赖标准库）。

它不是模型，也不会画图：按 prompt 的哈希取一个渐变色，当场编码成 64x64 PNG
返回。存在的意义是把"插件贡献媒体后端"需要的**最小协议面**跑通：

  - mcp.json 里声明一个普通的 stdio server；
  - plugin.json 的扩展里把 provider 名字绑到 server + tool；
  - 工具返回 {"b64": ...}，内核负责解码、落盘，模型只看到本地文件路径。

接真实厂商时，把 generate() 换成 HTTP 调用即可——返回值形状不用变。
"""

import base64
import hashlib
import json
import struct
import sys
import zlib

SIZE = 64

TOOLS = [
    {
        "name": "generate_image",
        "description": "Generate a 64x64 gradient PNG for the given prompt and return it as base64.",
        "inputSchema": {
            "type": "object",
            "properties": {
                "prompt": {"type": "string", "description": "Image description"},
                "size": {"type": "string", "description": "Ignored by this stub"},
            },
            "required": ["prompt"],
        },
    }
]


def png_chunk(tag, data):
    return (
        struct.pack(">I", len(data))
        + tag
        + data
        + struct.pack(">I", zlib.crc32(tag + data) & 0xFFFFFFFF)
    )


def gradient_png(prompt):
    """按 prompt 派生两个颜色，画一张 64x64 的 RGB 渐变。"""
    digest = hashlib.sha256((prompt or "empty").encode("utf-8")).digest()
    c0 = (digest[0], digest[1], digest[2])
    c1 = (digest[3], digest[4], digest[5])
    rows = b""
    for y in range(SIZE):
        row = bytearray([0])  # 每行前缀：filter type = None
        for x in range(SIZE):
            # 对角线性插值
            t = (x + y) / (2 * (SIZE - 1))
            row += bytes(
                int(c0[i] + (c1[i] - c0[i]) * t) for i in range(3)
            )
        rows += bytes(row)
    return (
        b"\x89PNG\r\n\x1a\n"
        + png_chunk(b"IHDR", struct.pack(">IIBBBBB", SIZE, SIZE, 8, 2, 0, 0, 0))
        + png_chunk(b"IDAT", zlib.compress(rows, 9))
        + png_chunk(b"IEND", b"")
    )


def text_result(payload):
    return {"content": [{"type": "text", "text": json.dumps(payload)}]}


def handle(req):
    method = req.get("method")
    req_id = req.get("id")
    if method is None or method.startswith("notifications/"):
        return None

    if method == "initialize":
        params = req.get("params") or {}
        return {
            "jsonrpc": "2.0",
            "id": req_id,
            "result": {
                "protocolVersion": params.get("protocolVersion", "2025-06-18"),
                "capabilities": {"tools": {"listChanged": False}},
                "serverInfo": {"name": "stubgen", "version": "1.0.0"},
            },
        }
    if method == "ping":
        return {"jsonrpc": "2.0", "id": req_id, "result": {}}
    if method == "tools/list":
        return {"jsonrpc": "2.0", "id": req_id, "result": {"tools": TOOLS}}
    if method == "tools/call":
        params = req.get("params") or {}
        if params.get("name") != "generate_image":
            return {
                "jsonrpc": "2.0",
                "id": req_id,
                "error": {"code": -32602, "message": "unknown tool"},
            }
        args = params.get("arguments") or {}
        png = gradient_png(args.get("prompt", ""))
        # format 可选：内核也会自己嗅探，写明是为了日志里一眼能看懂。
        return {
            "jsonrpc": "2.0",
            "id": req_id,
            "result": text_result(
                {"b64": base64.b64encode(png).decode("ascii"), "format": "png"}
            ),
        }
    return {
        "jsonrpc": "2.0",
        "id": req_id,
        "error": {"code": -32601, "message": "method not found"},
    }


def main():
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

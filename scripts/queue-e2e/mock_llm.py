"""Minimal OpenAI-compatible mock used to verify queue execution end to end.

Why a mock instead of a real provider:
    The regression we are checking is *orchestration* -- whether a queued
    message is ever handed to the agent after the previous turn finishes.
    That is independent of which model answers, so a deterministic local
    model removes network flakiness and API cost from the equation.

Behaviour: any /v1/chat/completions POST streams back a short SSE reply that
echoes the last user message, so the persisted history proves which queued
turn actually executed.

Pacing (SLOW_MS / SLOW_MATCH):
    Several checks need the FIRST turn to still be running while more messages
    are submitted -- that is the only way to observe a non-empty queue. A mock
    that replies instantly drains the queue before the test can look at it,
    which looks exactly like "the queue is broken".

    So: if the incoming text contains SLOW_MATCH, stream with a long per-token
    delay. The default is deliberately spacious (a few seconds per chunk) so
    the queue stays observable; normal messages stay fast so the suite does
    not crawl.

    Env overrides:
        MOCK_SLOW_MATCH  substring that triggers slow streaming (default "msg-running")
        MOCK_SLOW_MS     per-chunk delay in ms when slow (default 900)
        MOCK_SLOW_CHUNKS chunks to emit when slow (default 12)
        MOCK_TOOL_MATCH  substring that triggers a tool-call reply (default "msg-tools")

Tool-call mode (MOCK_TOOL_MATCH):
    The execution dock only renders when a turn actually invokes tools, so a
    text-only mock can never exercise it. When the incoming text contains
    MOCK_TOOL_MATCH the mock emits a sequence of tool_calls deltas (one per
    tick), then a final text answer. The tool names / arguments are chosen to
    hit several display branches at once: a command, a file path, a search
    pattern, and one deliberately failing call.
"""

import json
import os
import sys
import threading
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

PORT = int(sys.argv[1]) if len(sys.argv) > 1 else 59999

SLOW_MATCH = os.environ.get("MOCK_SLOW_MATCH", "msg-running")
SLOW_MS = int(os.environ.get("MOCK_SLOW_MS", "900"))
SLOW_CHUNKS = int(os.environ.get("MOCK_SLOW_CHUNKS", "12"))
TOOL_MATCH = os.environ.get("MOCK_TOOL_MATCH", "msg-tools")
TOOL_MS = int(os.environ.get("MOCK_TOOL_MS", "400"))

# Each entry becomes one tool_call; `arguments` is a JSON string so the client
# has to parse it exactly like it would for a real provider.
#
# Keep these to tools that are SAFE TO ACTUALLY EXECUTE in an isolated home:
# the agent really runs whatever the model asks for, so a tool that prompts for
# approval (or touches the network) will hang the turn forever instead of
# exercising the UI. These four cover the display branches we care about --
# a command, a file path, a search pattern, and a relative path -- without
# blocking on user input.
TOOL_SCRIPT = [
    {"name": "bash", "arguments": '{"command": "echo dock-check"}'},
    {"name": "Read", "arguments": '{"file_path": "/dev/null"}'},
    {"name": "Glob", "arguments": '{"pattern": "**/*.md", "path": "."}'},
    {"name": "Grep", "arguments": '{"pattern": "taskDockVisible", "path": "."}'},
    {"name": "ListDir", "arguments": '{"path": "."}'},
    {"name": "WebSearch", "arguments": '{"query": "go-magic agent framework"}'},
    {"name": "TodoWrite", "arguments": '{"todos": [{"task": "check dock", "done": true}]}'},
    {"name": "bash", "arguments": '{"command": "go version"}'},
    {"name": "Read", "arguments": '{"file_path": "README.md"}'},
    {"name": "Grep", "arguments": '{"pattern": "clearPending", "path": "internal/server"}'},
    {"name": "Glob", "arguments": '{"pattern": "internal/server/*.go"}'},
    {"name": "TodoWrite", "arguments": '{"todos": [{"task": "verify dock", "done": false}]}'},
]


def sse_chunk(piece_delta, finish=None):
    return {
        "id": "chatcmpl-mock",
        "object": "chat.completion.chunk",
        "choices": [
            {"index": 0, "delta": piece_delta, "finish_reason": finish}
        ],
    }


class Handler(BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"

    def log_message(self, *args):
        pass  # keep the console clean

    def do_POST(self):
        length = int(self.headers.get("Content-Length") or 0)
        raw = self.rfile.read(length) if length else b"{}"
        try:
            payload = json.loads(raw.decode() or "{}")
        except Exception:
            payload = {}

        # Find the last user message so the reply is traceable back to it.
        last_user = ""
        for m in payload.get("messages", []):
            if m.get("role") == "user":
                c = m.get("content")
                if isinstance(c, str):
                    last_user = c
                elif isinstance(c, list):
                    for part in c:
                        if isinstance(part, dict) and part.get("type") == "text":
                            last_user = part.get("text", "")
        reply = f"mock-reply to [{last_user[:60]}]"

        if not self.path.endswith("/chat/completions"):
            body = json.dumps({"error": "unsupported"}).encode()
            self.send_response(404)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(body)))
            self.end_headers()
            self.wfile.write(body)
            return

        # Stream as OpenAI-style SSE, split into a few chunks so the server's
        # delta handling is genuinely exercised.
        self.send_response(200)
        self.send_header("Content-Type", "text/event-stream")
        self.send_header("Cache-Control", "no-cache")
        self.send_header("Connection", "keep-alive")
        self.end_headers()

        def emit(frame):
            try:
                self.wfile.write(f"data: {json.dumps(frame)}\n\n".encode())
                self.wfile.flush()
                return True
            except (BrokenPipeError, ConnectionResetError):
                return False

        # Tool-call path: emit one tool_call per tick so the dock can be seen
        # accumulating items instead of appearing all at once.
        #
        # Wire format matters here: the ID-bearing frame *opens* a tool call and
        # the server keys new calls off `id != ""`; follow-up frames for the same
        # index must omit the id and only carry the argument continuation.
        # Echoing the id on every frame makes the server treat each chunk as a
        # brand-new call and reset the accumulated arguments, so the turn never
        # produces a callable invocation (symptom: tools stuck at "running" and
        # the reply shows `tool_calls` that were never executed).
        if TOOL_MATCH and TOOL_MATCH in last_user:
            for idx, spec in enumerate(TOOL_SCRIPT):
                # Opening frame: id + name, arguments as the first fragment.
                args = spec["arguments"]
                mid = max(1, len(args) // 2)
                open_delta = {
                    "tool_calls": [
                        {
                            "index": idx,
                            "id": f"call_mock_{idx}",
                            "type": "function",
                            "function": {"name": spec["name"], "arguments": args[:mid]},
                        }
                    ]
                }
                if not emit(sse_chunk(open_delta)):
                    return
                threading.Event().wait(TOOL_MS / 1000.0)

                # Continuation frame: same index, NO id, rest of the arguments.
                cont_delta = {
                    "tool_calls": [
                        {
                            "index": idx,
                            "function": {"arguments": args[mid:]},
                        }
                    ]
                }
                if not emit(sse_chunk(cont_delta)):
                    return
                threading.Event().wait(TOOL_MS / 1000.0)

            # Final assistant text after the tools, so the dock must switch to
            # the synthesis phase.
            for piece in ["done", " with", " tools"]:
                if not emit(sse_chunk({"content": piece})):
                    return
                threading.Event().wait(TOOL_MS / 1000.0)

            emit(sse_chunk({}, finish="stop"))
            try:
                self.wfile.write(b"data: [DONE]\n\n")
                self.wfile.flush()
            except (BrokenPipeError, ConnectionResetError):
                pass
            return

        # Slow path: hold the turn open long enough for the test to pile up
        # queued messages behind it and inspect /running.
        slow = SLOW_MATCH and SLOW_MATCH in last_user
        if slow:
            # Keep the echoed text so the persisted answer stays traceable
            # back to the request that produced it.
            words = [f"{reply}#{i}" for i in range(SLOW_CHUNKS)]
            delay = SLOW_MS / 1000.0
        else:
            words = reply.split(" ")
            delay = 0.01

        for i, w in enumerate(words):
            piece = w if i == 0 else " " + w
            frame = {
                "id": "chatcmpl-mock",
                "object": "chat.completion.chunk",
                "choices": [{"index": 0, "delta": {"content": piece}, "finish_reason": None}],
            }
            try:
                self.wfile.write(f"data: {json.dumps(frame)}\n\n".encode())
                self.wfile.flush()
            except (BrokenPipeError, ConnectionResetError):
                # Client walked away (cancelled turn) -- nothing to do.
                return
            threading.Event().wait(delay)

        final = {
            "id": "chatcmpl-mock",
            "object": "chat.completion.chunk",
            "choices": [{"index": 0, "delta": {}, "finish_reason": "stop"}],
            "usage": {"prompt_tokens": 10, "completion_tokens": 5, "total_tokens": 15},
        }
        try:
            self.wfile.write(f"data: {json.dumps(final)}\n\n".encode())
            self.wfile.write(b"data: [DONE]\n\n")
            self.wfile.flush()
        except (BrokenPipeError, ConnectionResetError):
            pass

    def do_GET(self):
        body = json.dumps({"object": "list", "data": [{"id": "mock-model", "object": "model"}]}).encode()
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)


if __name__ == "__main__":
    srv = ThreadingHTTPServer(("127.0.0.1", PORT), Handler)
    print(f"mock llm listening on 127.0.0.1:{PORT}", flush=True)
    srv.serve_forever()

"""E2E verification for: "消息执行完后，队列消息没有自动发送".

This drives a real server over HTTP and asserts the *streaming* contract that
the fix targets:

  - Submit message A on a fresh SSE connection.
  - While A is running, submit message B (goes into the same session queue).
  - The SAME SSE connection must carry turn B's `stream_started` and its
    deltas, because that is what makes the UI actually render B.

Before the fix, the server's forwardTurnEvents returned on the first `done`,
which tore down the sink; B then ran with zero listeners and its events were
never delivered -- the user's "排队消息没有自动发送".

Usage:
    python verify_multiturn_stream.py <base_url> <password>
"""

import json
import sys
import threading
import time
import urllib.error
import urllib.request

BASE = sys.argv[1].rstrip("/")
PASSWORD = sys.argv[2] if len(sys.argv) > 2 else "verify-password-123"

PASS = []
FAIL = []
TOKEN = ""


def check(name, ok, detail=""):
    (PASS if ok else FAIL).append(name)
    print(f"[{'PASS' if ok else 'FAIL'}] {name}" + (f"  -- {detail}" if detail and not ok else ""), flush=True)


def call(method, path, body=None, timeout=30, token=None):
    tok = token if token is not None else TOKEN
    headers = {"Accept": "application/json"}
    if tok:
        headers["Authorization"] = f"Bearer {tok}"
    data = None
    if body is not None:
        data = json.dumps(body).encode()
        headers["Content-Type"] = "application/json"
    req = urllib.request.Request(f"{BASE}{path}", data=data, headers=headers, method=method)
    try:
        with urllib.request.urlopen(req, timeout=timeout) as r:
            raw = r.read().decode()
            return r.status, (json.loads(raw) if raw.strip() else {})
    except urllib.error.HTTPError as e:
        raw = e.read().decode()
        try:
            return e.code, json.loads(raw)
        except Exception:
            return e.code, {"raw": raw}


def login():
    global TOKEN
    call("POST", "/api/auth/setup", {"password": PASSWORD}, token="")
    status, res = call("POST", "/api/auth/login", {"password": PASSWORD}, token="")
    TOKEN = res.get("token") or ""
    return bool(TOKEN)


class StreamCollector:
    """Consumes one SSE connection and records every parsed frame, in order."""

    def __init__(self, session_id):
        self.session_id = session_id
        self.frames = []
        self.lock = threading.Lock()
        self.started = threading.Event()
        self.finished = threading.Event()
        self.thread = threading.Thread(target=self._run, daemon=True)

    def start(self):
        self.thread.start()
        return self

    def _run(self):
        headers = {
            "Accept": "text/event-stream",
            "Authorization": f"Bearer {TOKEN}",
            "Content-Type": "application/json",
        }
        body = json.dumps({"content": "turn alpha"}).encode()
        req = urllib.request.Request(
            f"{BASE}/api/sessions/{self.session_id}/stream",
            data=body,
            headers=headers,
            method="POST",
        )
        try:
            with urllib.request.urlopen(req, timeout=180) as r:
                self.started.set()
                buf = ""
                while True:
                    chunk = r.read(1)
                    if not chunk:
                        break
                    buf += chunk.decode("utf-8", "replace")
                    while "\n\n" in buf:
                        frame, buf = buf.split("\n\n", 1)
                        for line in frame.splitlines():
                            if line.startswith("data:"):
                                try:
                                    obj = json.loads(line[5:].strip())
                                except Exception:
                                    continue
                                with self.lock:
                                    self.frames.append(obj)
        except Exception as e:
            with self.lock:
                self.frames.append({"__error": str(e)})
        finally:
            self.finished.set()

    def snapshot(self):
        with self.lock:
            return list(self.frames)


def main():
    if not login():
        print("login failed")
        sys.exit(2)

    status, sess = call("POST", "/api/sessions", {"title": "multiturn-stream"})
    if status not in (200, 201) or not sess.get("id"):
        print(f"session create failed: {status} {sess}")
        sys.exit(2)
    sid = sess["id"]
    print(f"session = {sid}")

    # --- turn A on a live SSE connection -------------------------------
    collector = StreamCollector(sid).start()
    collector.started.wait(timeout=20)

    # Wait until the server reports A as running.
    for _ in range(60):
        _, running = call("GET", f"/api/sessions/{sid}/running")
        if running.get("running"):
            break
        time.sleep(0.25)

    # --- turn B goes into the same queue, same session -----------------
    status, resp = call("POST", f"/api/sessions/{sid}/messages", {"content": "turn beta"})
    check("第二条消息成功入队", status in (200, 201) and bool(resp.get("id")), f"{status} {resp}")
    beta_id = str(resp.get("id") or "")

    # --- wait for the queue to drain ----------------------------------
    deadline = time.time() + 150
    final = {}
    while time.time() < deadline:
        _, running = call("GET", f"/api/sessions/{sid}/running")
        final = running
        if not running.get("running") and int(running.get("queue_depth") or 0) == 0:
            break
        time.sleep(0.5)

    time.sleep(2.0)
    frames = collector.snapshot()

    # ---------------------------------------------------------------
    # The decisive assertions: B's events must have arrived on the SAME
    # connection that served A.
    # ---------------------------------------------------------------
    starts = [f for f in frames if f.get("type") == "stream_started" and f.get("started") is not False]
    start_ids = [str(f.get("id") or "") for f in starts]
    done_frames = [f for f in frames if f.get("done")]
    done_ids = [str(f.get("turn_id") or "") for f in done_frames]

    check(
        "同一个连接上收到了两个回合的开始事件",
        len(starts) >= 2,
        f"stream_started ids = {start_ids}",
    )
    check(
        "第二个回合确实在队列里（id 对得上）",
        beta_id in start_ids,
        f"beta_id={beta_id} starts={start_ids}",
    )
    check(
        "同一个连接上收到了两个回合的结束事件",
        len(done_frames) >= 2,
        f"done turn_ids = {done_ids}",
    )
    check(
        "第二个回合产生了流式内容",
        len([f for f in frames if f.get("delta")]) >= 2,
        f"delta frames = {len([f for f in frames if f.get('delta')])}",
    )

    # done 帧必须带 queue_idle，连接才有收尾依据
    if done_frames:
        last_done = done_frames[-1]
        check(
            "最后一个 done 带 queue_idle=true（连接可安全收尾）",
            last_done.get("queue_idle") is True,
            f"last done = {last_done}",
        )

    check(
        "队列最终排空",
        not final.get("running") and int(final.get("queue_depth") or 0) == 0,
        f"final = {final}",
    )

    # And both messages should be in history.
    _, msgs = call("GET", f"/api/sessions/{sid}/messages")
    history = msgs.get("messages", []) if isinstance(msgs, dict) else []
    users = [m.get("content", "") for m in history if m.get("role") == "user"]
    assistants = [m for m in history if m.get("role") == "assistant"]
    check("两条用户消息都已落库", any("alpha" in u for u in users) and any("beta" in u for u in users), f"users = {users}")
    check("两条回答都已落库", len(assistants) >= 2, f"assistant count = {len(assistants)}")

    print("")
    print(f"passed {len(PASS)} / {len(PASS) + len(FAIL)}")
    if FAIL:
        print("failures:")
        for n in FAIL:
            print(f"  - {n}")
        sys.exit(1)


if __name__ == "__main__":
    main()

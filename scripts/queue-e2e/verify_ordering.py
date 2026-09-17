"""E2E check for message ordering under queueing.

User report: "排版顺序混乱" -- after the queue started auto-sending, the
conversation order came out jumbled (an assistant reply appeared before the
reply that logically preceded it).

The frontend bug it pins down:
    On `done`, the assistant message used to be pushed inside `nextTick(...)`
    -- a deferred callback. But `stream_started` for the NEXT queued turn
    calls promoteQueuedToMessage SYNCHRONOUSLY. So if turn 2's start landed
    before turn 1's nextTick flushed, turn 2's user message was spliced in
    BEFORE turn 1's assistant reply, producing:
        user1, user2, assistant1, assistant2      <- wrong
    instead of:
        user1, assistant1, user2, assistant2      <- right

This script verifies the authoritative server-side ordering (user/assistant
strictly alternating) for a burst of three messages, which is the contract
the frontend's optimistic list must converge to.

Usage:
    python verify_ordering.py <base_url> <password>
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


def drain_stream(session_id, body):
    """Open a stream and read it to completion in a background thread."""
    def run():
        headers = {
            "Accept": "text/event-stream",
            "Authorization": f"Bearer {TOKEN}",
            "Content-Type": "application/json",
        }
        req = urllib.request.Request(
            f"{BASE}/api/sessions/{session_id}/stream",
            data=json.dumps(body).encode(),
            headers=headers,
            method="POST",
        )
        try:
            with urllib.request.urlopen(req, timeout=200) as r:
                while True:
                    if not r.read(4096):
                        break
        except Exception:
            pass

    t = threading.Thread(target=run, daemon=True)
    t.start()
    return t


def main():
    if not login():
        print("login failed")
        sys.exit(2)

    status, sess = call("POST", "/api/sessions", {"title": "ordering"})
    if status not in (200, 201) or not sess.get("id"):
        print(f"session create failed: {status} {sess}")
        sys.exit(2)
    sid = sess["id"]
    print(f"session = {sid}")

    texts = ["msg-alpha", "msg-beta", "msg-gamma"]

    # First message opens the connection; the rest queue up while it runs.
    drain_stream(sid, {"content": texts[0]})
    time.sleep(1.5)
    for t in texts[1:]:
        status, resp = call("POST", f"/api/sessions/{sid}/messages", {"content": t})
        check(f"入队 {t}", status in (200, 201), f"{status} {resp}")
        time.sleep(0.4)

    deadline = time.time() + 180
    final = {}
    while time.time() < deadline:
        _, running = call("GET", f"/api/sessions/{sid}/running")
        final = running
        if not running.get("running") and int(running.get("queue_depth") or 0) == 0:
            break
        time.sleep(0.5)

    check("队列排空", not final.get("running") and int(final.get("queue_depth") or 0) == 0, f"{final}")

    _, msgs = call("GET", f"/api/sessions/{sid}/messages")
    history = msgs.get("messages", []) if isinstance(msgs, dict) else []
    roles = [m.get("role") for m in history]
    print(f"roles = {roles}")

    # 1) Strict alternation: user, assistant, user, assistant, ...
    alternating = all(
        roles[i] == ("user" if i % 2 == 0 else "assistant") for i in range(len(roles))
    )
    check("角色严格交替 user/assistant（顺序未错乱）", alternating, f"roles = {roles}")

    # 2) The user messages must appear in submission order.
    users = [m.get("content", "") for m in history if m.get("role") == "user"]
    ordered = all(t in users[i] for i, t in enumerate(texts) if i < len(users))
    check("三条用户消息按提交顺序出现", ordered, f"users = {users}")

    # 3) No two users adjacent (that is what a deferred assistant push causes).
    adjacent_users = any(roles[i] == "user" and roles[i + 1] == "user" for i in range(len(roles) - 1))
    check("没有两条 user 相邻（回答未丢失/未错位）", not adjacent_users, f"roles = {roles}")

    print("")
    print(f"passed {len(PASS)} / {len(PASS) + len(FAIL)}")
    if FAIL:
        print("failures:")
        for n in FAIL:
            print(f"  - {n}")
        sys.exit(1)


if __name__ == "__main__":
    main()

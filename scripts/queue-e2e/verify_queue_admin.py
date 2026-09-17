"""E2E check for the two queue affordances added on 2026-09-17:

  1. POST /api/sessions/{id}/queue/clear
     -- drop the pending queue but let the RUNNING turn finish.
        Distinct from /cancel (which kills the running turn too).

  2. GET  /api/sessions/{id}/queue/{turnId}/content
     -- return the FULL text of a queued message, not the 120-char preview
        that /running ships. Needed because "edit a queued message" refills
        the composer, and after a page refresh the browser no longer holds
        the original string.

Route dispatch is the real risk here, and it is why this runs end-to-end
rather than as a unit test: both new paths hang off the existing
`/queue/` prefix that otherwise captures everything as `{turnId}`. If the
ordering of the `HasSuffix` checks regresses, `/queue/clear` silently
becomes "delete the queued message whose id is literally 'clear'" and
returns a plausible-looking 200 -- a unit test on the handler would never
notice.

Usage:
    python verify_queue_admin.py <base_url> <password>
"""

import json
import os
import sys
import threading
import time
import urllib.error
import urllib.request

BASE = sys.argv[1].rstrip("/")
PASSWORD = sys.argv[2] if len(sys.argv) > 2 else "verify-password-123"

# Must match the mock's MOCK_SLOW_* settings (see mock_llm.py). The slow turn
# emits this many numbered chunks, which is what lets us tell a completed
# answer from one that was cancelled halfway.
SLOW_CHUNKS = int(os.environ.get("MOCK_SLOW_CHUNKS", "12"))

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


def open_stream(session_id, body, sink):
    """Open a stream in the background, recording every SSE frame it sees."""
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
                buf = b""
                while True:
                    chunk = r.read(1024)
                    if not chunk:
                        break
                    buf += chunk
                    while b"\n\n" in buf:
                        frame, buf = buf.split(b"\n\n", 1)
                        sink.append(frame.decode("utf-8", "replace"))
        except Exception:
            pass

    t = threading.Thread(target=run, daemon=True)
    t.start()
    return t


def new_session(title):
    status, sess = call("POST", "/api/sessions", {"title": title})
    if status not in (200, 201) or not sess.get("id"):
        print(f"session create failed: {status} {sess}")
        sys.exit(2)
    return sess["id"]


def wait_idle(sid, timeout=180):
    """Wait until the queue is fully drained (no running turn, no pending)."""
    deadline = time.time() + timeout
    last = {}
    while time.time() < deadline:
        _, last = call("GET", f"/api/sessions/{sid}/running")
        if not last.get("running") and int(last.get("queue_depth") or 0) == 0:
            return True, last
        time.sleep(0.4)
    return False, last


def wait_queue_depth(sid, want, timeout=30):
    """Wait until the pending depth reaches `want` (the running turn is excluded)."""
    deadline = time.time() + timeout
    last = {}
    while time.time() < deadline:
        _, last = call("GET", f"/api/sessions/{sid}/running")
        if int(last.get("queue_depth") or 0) == want:
            return True, last
        time.sleep(0.2)
    return False, last


# --------------------------------------------------------------------------
# Part 1: /queue/clear drops pending items but keeps the running turn alive.
# --------------------------------------------------------------------------
def test_queue_clear():
    print("\n--- 1. POST /queue/clear 只清待发，不打断当前回合 ---")
    sid = new_session("clear-pending")
    print(f"session = {sid}")

    frames = []
    # The first message is paced to stream slowly by the mock (see mock_llm.py's
    # MOCK_SLOW_MATCH), so the turn is still running while we pile messages
    # behind it. Without that pacing the queue drains before we can look at it
    # -- which is indistinguishable from "the queue is broken".
    open_stream(sid, {"content": "msg-running"}, frames)
    time.sleep(3.0)

    # Queue up three more while the first still runs.
    for t in ("msg-queued-1", "msg-queued-2", "msg-queued-3"):
        status, resp = call("POST", f"/api/sessions/{sid}/messages", {"content": t})
        check(f"入队 {t}", status in (200, 201), f"{status} {resp}")
        time.sleep(0.3)

    ok, snap = wait_queue_depth(sid, 3)
    check("排队深度达到 3", ok, f"{snap}")
    running_before = bool(snap.get("running"))
    check("此时确实有一个回合在跑", running_before, f"{snap}")

    # The operation under test.
    status, res = call("POST", f"/api/sessions/{sid}/queue/clear")
    check("/queue/clear 返回 200", status == 200, f"{status} {res}")
    check("/queue/clear 报告丢弃 3 条", int(res.get("dropped") or 0) == 3, f"{res}")

    # The running turn must NOT have been cancelled.
    #
    # Careful: polling /running ONCE right after the call is not enough. When
    # clearPending is (incorrectly) implemented like cancelAll, the cancel
    # takes a moment to propagate -- a single immediate read still sees
    # running=true and the check passes against buggy code. That false
    # negative is exactly what happened the first time this suite was run
    # against a deliberately reverted build.
    #
    # So assert on the OUTCOME instead, which is what the user actually cares
    # about: the slow-paced turn must produce its FULL reply. The mock emits
    # MOCK_SLOW_CHUNKS chunks for "msg-running"; a cancelled turn keeps only
    # the prefix that had already streamed.
    _, after = call("GET", f"/api/sessions/{sid}/running")
    check(
        "清空待发后当前回合仍在跑（未被取消）",
        bool(after.get("running")),
        f"清空后 running={after.get('running')} —— 若为 false 说明 /queue/clear 退化成 /cancel 了",
    )
    check("清空后队列深度为 0", int(after.get("queue_depth") or 0) == 0, f"{after}")

    # And it must run to completion with its answer persisted.
    ok, final = wait_idle(sid)
    check("当前回合最终跑完", ok, f"{final}")

    _, msgs = call("GET", f"/api/sessions/{sid}/messages")
    history = msgs.get("messages", []) if isinstance(msgs, dict) else []
    roles = [m.get("role") for m in history]
    users = [m.get("content", "") for m in history if m.get("role") == "user"]
    print(f"roles = {roles}")
    print(f"users = {users}")

    # The decisive anti-regression assertion: the slow turn streamed a known
    # number of chunks, so a cancelled turn is detectable by a short answer.
    # Without this, reverting clearPending to cancelAll still passes.
    answer = "\n".join(m.get("content", "") for m in history if m.get("role") == "assistant")
    expected_marker = "#" + str(SLOW_CHUNKS - 1)
    check(
        f"当前回合的回答是完整的（含末块 {expected_marker}，证明未被中途取消）",
        expected_marker in answer,
        f"回答缺少末块 {expected_marker} —— 说明回合被 /queue/clear 误取消了。answer 尾部={answer[-60:]!r}",
    )

    check("被清空的三条从未落库（历史里没有它们）",
          not any("msg-queued" in u for u in users), f"users = {users}")
    check("当前这条仍然落库并跑完了回答",
          any("msg-running" in u for u in users) and "assistant" in roles, f"roles={roles} users={users}")

    # The cleared queue must not be reported by /running afterwards.
    _, tail = call("GET", f"/api/sessions/{sid}/running")
    check("清空后 /running 不再报告任何排队项",
          int(tail.get("queue_depth") or 0) == 0 and not tail.get("queued"), f"{tail}")


# --------------------------------------------------------------------------
# Part 2: /queue/{turnId}/content returns the UNTRUNCATED text.
# --------------------------------------------------------------------------
def test_queue_content_full_text():
    print("\n--- 2. GET /queue/{turnId}/content 返回未截断原文 ---")
    sid = new_session("full-content")
    print(f"session = {sid}")

    # Occupy the worker with a slow-streaming turn so the next message stays
    # queued long enough to be inspected.
    open_stream(sid, {"content": "msg-running holder"}, [])
    time.sleep(3.0)

    # 400 Chinese characters: well past the 120-char preview cap. If the
    # endpoint regresses to the /running preview, the tail will be missing.
    long_text = "长文本测试" * 80
    check("测试文本超过预览上限 120 字", len(long_text) > 120, f"len={len(long_text)}")

    status, resp = call("POST", f"/api/sessions/{sid}/messages", {"content": long_text})
    check("长文本入队成功", status in (200, 201), f"{status} {resp}")
    ok, snap = wait_queue_depth(sid, 1)
    check("长文本处于排队状态", ok, f"{snap}")

    queued = (snap.get("queued") or [{}])[0]
    turn_id = queued.get("id") or ""
    preview = queued.get("content") or ""
    check("拿到排队项 id", bool(turn_id), f"{queued}")
    check("列表预览确实被截断（带省略号）", preview.endswith("…"), f"preview={preview!r}")

    # The operation under test.
    status, full = call("GET", f"/api/sessions/{sid}/queue/{turn_id}/content")
    check("/queue/{id}/content 返回 200", status == 200, f"{status} {full}")
    got = full.get("content") or ""
    check("返回的是完整原文（与提交的字符串逐字相同）", got == long_text,
          f"len(got)={len(got)} len(want)={len(long_text)} got_tail={got[-20:]!r}")
    check("返回的原文不再带省略号", not got.endswith("…"), f"tail={got[-10:]!r}")

    # Unknown id -> 404 (the frontend turns this into "too late to edit").
    status, miss = call("GET", f"/api/sessions/{sid}/queue/no-such-turn/content")
    check("未知 turnId 返回 404", status == 404, f"{status} {miss}")

    # Cleanup so the next part starts from a known state.
    call("POST", f"/api/sessions/{sid}/queue/clear")
    wait_idle(sid)


# --------------------------------------------------------------------------
# Part 3: route dispatch -- "/queue/clear" must not be eaten as a turnId.
# --------------------------------------------------------------------------
def test_route_dispatch():
    print("\n--- 3. 路由分派：/queue/clear 不被当成 turnId ---")
    sid = new_session("route-dispatch")
    print(f"session = {sid}")

    # With an empty queue, the DELETE-style fallback (handleSessionQueueItem)
    # would answer with removed=false, while clear answers with dropped=0.
    # Requiring the `dropped` key proves which handler actually ran.
    status, res = call("POST", f"/api/sessions/{sid}/queue/clear")
    check("空队列上 POST /queue/clear 返回 200", status == 200, f"{status} {res}")
    check("响应带 dropped 字段（证明命中 clear handler 而非 item handler）",
          "dropped" in res, f"{res}")

    # GET on clear must be rejected -- it is a POST-only endpoint.
    status, res = call("GET", f"/api/sessions/{sid}/queue/clear")
    check("GET /queue/clear 返回 405（方法受限）", status == 405, f"{status} {res}")


def main():
    if not login():
        print("login failed")
        sys.exit(2)

    test_queue_clear()
    test_queue_content_full_text()
    test_route_dispatch()

    print("")
    print(f"passed {len(PASS)} / {len(PASS) + len(FAIL)}")
    if FAIL:
        print("failures:")
        for n in FAIL:
            print(f"  - {n}")
        sys.exit(1)


if __name__ == "__main__":
    main()

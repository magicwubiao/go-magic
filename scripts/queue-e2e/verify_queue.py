#!/usr/bin/env python3
"""End-to-end check of the queue reorder + retry endpoints.

Drives the real server over HTTP. Two things make this non-obvious:

1. The queue worker starts draining immediately, so the head of the queue is
   very quickly claimed and leaves the pending list. Tests therefore must not
   assume "3 submitted == 3 pending"; the first turn is usually already running.
   Reorder assertions are made against the PENDING items only, and we compare
   relative order rather than absolute indices.

2. `retry_of` exists precisely to defeat the duplicate check. Without it,
   resubmitting identical content collapses into the existing item and the
   queue does not change at all -- which is the bug the field fixes.

Run:  python verify_queue.py <base_url> <token>
"""
import json
import sys
import time
import urllib.error
import urllib.request

BASE = sys.argv[1].rstrip("/")
TOKEN = sys.argv[2] if len(sys.argv) > 2 else ""


def call(method, path, body=None):
    headers = {"Accept": "application/json"}
    if TOKEN:
        headers["Authorization"] = f"Bearer {TOKEN}"
    data = None
    if body is not None:
        data = json.dumps(body).encode()
        headers["Content-Type"] = "application/json"
    req = urllib.request.Request(f"{BASE}{path}", data=data, headers=headers, method=method)
    try:
        with urllib.request.urlopen(req, timeout=30) as resp:
            raw = resp.read().decode()
            return resp.status, (json.loads(raw) if raw.strip() else {})
    except urllib.error.HTTPError as e:
        raw = e.read().decode()
        try:
            return e.code, json.loads(raw)
        except Exception:
            return e.code, {"raw": raw}


def pending(session_id):
    _, res = call("GET", f"/api/sessions/{session_id}/running")
    return res.get("queued", []), res.get("active_id", "")


def contents(items):
    return [q["content"] for q in items]


def main():
    failures = []

    def check(name, cond, detail=""):
        print(f"[{'PASS' if cond else 'FAIL'}] {name}" + (f"  -- {detail}" if detail else ""))
        if not cond:
            failures.append(name)

    status, sess = call("POST", "/api/sessions", {"title": "queue-verify"})
    if status >= 400 or not sess.get("id"):
        print(f"cannot create session: {status} {sess}")
        return 1
    sid = sess["id"]
    print(f"session: {sid}\n")

    # Submit several distinct messages in quick succession.
    ids = {}
    for text in ("m1", "m2", "m3", "m4", "m5"):
        status, res = call("POST", f"/api/sessions/{sid}/messages", {"content": text})
        if status >= 400:
            print(f"submit {text!r} failed: {status} {res}")
            return 1
        ids[text] = res.get("id")
    print(f"submitted: {list(ids)}\n")

    items, active = pending(sid)
    print(f"active={active!r} pending={contents(items)}\n")

    # The worker claims the head almost immediately; whatever is left must
    # still be in submission order.
    check("pending is a suffix of submission order",
          contents(items) == [t for t in ("m1", "m2", "m3", "m4", "m5") if t in ids][-len(items):],
          f"{contents(items)}")
    check("submitted messages accounted for",
          len(items) + (1 if active else 0) >= 2,
          f"pending={len(items)} active={active!r}")
    check("positions are 1..n",
          [q["position"] for q in items] == list(range(1, len(items) + 1)),
          f"{[q['position'] for q in items]}")

    # Pick two pending items and swap them for real.
    if len(items) < 2:
        print("\nnot enough pending items to test reorder")
        return 1

    last = items[-1]
    first = items[0]

    # --- drag the last pending item to the head --------------------------
    status, res = call("PUT", f"/api/sessions/{sid}/queue/{last['id']}/position", {"to": 0})
    check("move-to-head returns 200", status == 200, f"status={status} {res}")
    check("move-to-head reports moved", res.get("moved") is True, f"{res}")
    after, _ = pending(sid)
    check("moved item is now first", after and after[0]["id"] == last["id"], f"{contents(after)}")

    # --- and back: drag the (now first) item to the tail -----------------
    status, res = call("PUT", f"/api/sessions/{sid}/queue/{last['id']}/position", {"to": 99})
    check("move-to-tail returns 200", status == 200, f"status={status} {res}")
    check("move-to-tail reports moved", res.get("moved") is True, f"{res}")
    after, _ = pending(sid)
    check("moved item is now last", after and after[-1]["id"] == last["id"], f"{contents(after)}")

    # --- no-op move must not report a move (avoids needless broadcasts) ---
    idx = [q["id"] for q in after].index(first["id"])
    status, res = call("PUT", f"/api/sessions/{sid}/queue/{first['id']}/position", {"to": idx})
    check("no-op move reports moved=false", res.get("moved") is False, f"{res}")

    # --- out-of-range target is clamped, not rejected --------------------
    status, res = call("PUT", f"/api/sessions/{sid}/queue/{first['id']}/position", {"to": 9999})
    check("out-of-range clamped (200)", status == 200, f"status={status}")
    after, _ = pending(sid)
    check("clamped to tail", after and after[-1]["id"] == first["id"], f"{contents(after)}")

    # --- unknown turn id -> moved=false, never a 5xx ---------------------
    status, res = call("PUT", f"/api/sessions/{sid}/queue/does-not-exist/position", {"to": 0})
    check("unknown id handled", status == 200 and res.get("moved") is False, f"{status} {res}")

    # --- retry: resubmitting identical content WITH retry_of must survive
    #     the duplicate check (that is the whole point of the field).
    before, _ = pending(sid)
    if not before:
        print("\nqueue drained before retry test")
        return 1
    target = before[0]
    status, res = call(
        "POST",
        f"/api/sessions/{sid}/messages",
        {"content": target["content"], "retry_of": target["id"]},
    )
    check("retry submit accepted", status < 400, f"status={status} {res}")
    check("retry is NOT reported as duplicate", res.get("duplicate") is False, f"{res}")
    check("retry got a NEW id", res.get("id") != target["id"], f"{res.get('id')}")
    after, _ = pending(sid)
    check("retry kept pending count stable",
          len(after) == len(before), f"before={contents(before)} after={contents(after)}")
    check("retry landed at the tail",
          after and after[-1]["id"] == res.get("id"), f"{contents(after)}")
    check("old id is gone from pending",
          target["id"] not in [q["id"] for q in after], f"{contents(after)}")

    # --- control: WITHOUT retry_of, identical content IS deduped ---------
    # This proves the duplicate check is still doing its job for normal
    # double-submits (e.g. a flaky network retry), and that retry_of is the
    # only thing that bypasses it.
    if after:
        dup_target = after[0]
        status, res = call(
            "POST",
            f"/api/sessions/{sid}/messages",
            {"content": dup_target["content"]},
        )
        check("plain resubmit IS deduped", res.get("duplicate") is True, f"{res}")
        check("dedupe returns the existing id",
              res.get("id") == dup_target["id"], f"{res.get('id')} vs {dup_target['id']}")

    print()
    if failures:
        print(f"{len(failures)} check(s) FAILED: {failures}")
        return 1
    print("all checks passed")
    return 0


if __name__ == "__main__":
    sys.exit(main())

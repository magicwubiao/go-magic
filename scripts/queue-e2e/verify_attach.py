#!/usr/bin/env python3
"""Verify that a queued message's attachments survive into the /running snapshot.

Why this matters: the frontend keeps queued attachments in an in-memory Map.
After a page refresh that Map is empty, so the ONLY source of truth for "this
queued message had a screenshot attached" is the server's queue snapshot. If
the snapshot omits attachments, refreshing loses them and pressing resend
silently drops the image.

Sequence: upload a real PNG -> queue a message referencing it -> read /running
-> assert the attachment came back with a usable url + mime.
"""
import json
import os
import sys
import urllib.error
import urllib.request
import uuid

BASE = sys.argv[1].rstrip("/")
TOKEN = sys.argv[2] if len(sys.argv) > 2 else ""

# Smallest valid 1x1 PNG.
PNG = bytes([
    0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A,
    0x00, 0x00, 0x00, 0x0D, 0x49, 0x48, 0x44, 0x52,
    0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
    0x08, 0x06, 0x00, 0x00, 0x00, 0x1F, 0x15, 0xC4,
    0x89, 0x00, 0x00, 0x00, 0x0A, 0x49, 0x44, 0x41,
    0x54, 0x78, 0x9C, 0x63, 0x00, 0x01, 0x00, 0x00,
    0x05, 0x00, 0x01, 0x0D, 0x0A, 0x2D, 0xB4, 0x00,
    0x00, 0x00, 0x00, 0x49, 0x45, 0x4E, 0x44, 0xAE,
    0x42, 0x60, 0x82,
])


def json_call(method, path, body=None):
    headers = {"Accept": "application/json"}
    if TOKEN:
        headers["Authorization"] = f"Bearer {TOKEN}"
    data = None
    if body is not None:
        data = json.dumps(body).encode()
        headers["Content-Type"] = "application/json"
    req = urllib.request.Request(f"{BASE}{path}", data=data, headers=headers, method=method)
    try:
        with urllib.request.urlopen(req, timeout=30) as r:
            raw = r.read().decode()
            return r.status, (json.loads(raw) if raw.strip() else {})
    except urllib.error.HTTPError as e:
        raw = e.read().decode()
        try:
            return e.code, json.loads(raw)
        except Exception:
            return e.code, {"raw": raw}


def upload(session_id, name, content):
    """multipart/form-data upload (urllib has no built-in multipart encoder)."""
    boundary = "----verify" + uuid.uuid4().hex
    parts = []
    parts.append(f"--{boundary}\r\n".encode())
    parts.append(
        f'Content-Disposition: form-data; name="file"; filename="{name}"\r\n'.encode()
    )
    parts.append(b"Content-Type: image/png\r\n\r\n")
    parts.append(content)
    parts.append(f"\r\n--{boundary}--\r\n".encode())
    payload = b"".join(parts)

    headers = {
        "Content-Type": f"multipart/form-data; boundary={boundary}",
        "Accept": "application/json",
    }
    if TOKEN:
        headers["Authorization"] = f"Bearer {TOKEN}"
    req = urllib.request.Request(
        f"{BASE}/api/upload", data=payload, headers=headers, method="POST"
    )
    try:
        with urllib.request.urlopen(req, timeout=30) as r:
            raw = r.read().decode()
            return r.status, (json.loads(raw) if raw.strip() else {})
    except urllib.error.HTTPError as e:
        raw = e.read().decode()
        try:
            return e.code, json.loads(raw)
        except Exception:
            return e.code, {"raw": raw}


failures = []


def check(name, cond, detail=""):
    print(f"[{'PASS' if cond else 'FAIL'}] {name}" + (f"  -- {detail}" if detail else ""))
    if not cond:
        failures.append(name)


status, sess = json_call("POST", "/api/sessions", {"title": "queue-attach-verify"})
if status >= 400:
    print(f"cannot create session: {status} {sess}")
    sys.exit(1)
sid = sess["id"]
print(f"session: {sid}\n")

status, up = upload(sid, "queued-shot.png", PNG)
print(f"upload -> {status} {up}")
if status >= 400 or not up.get("url"):
    print("upload failed; cannot continue")
    sys.exit(1)

file_url = up["url"]

# Queue the image plus a caption. `images` carries the multimodal payload and
# `imageUrls` carries the persisted reference -- both are needed, matching what
# the real frontend sends.
status, res = json_call(
    "POST",
    f"/api/sessions/{sid}/messages",
    {
        "content": "look at this",
        "images": ["data:image/png;base64," + __import__("base64").b64encode(PNG).decode()],
        "imageUrls": [file_url],
        "imageNames": ["queued-shot.png"],
    },
)
check("submit with attachment accepted", status < 400, f"status={status} {res}")

status, run = json_call("GET", f"/api/sessions/{sid}/running")
items = run.get("queued", [])
print(f"\n/running pending={[q.get('content') for q in items]}")
print(json.dumps(items, ensure_ascii=False, indent=2))

mine = next((q for q in items if q.get("content") == "look at this"), None)

# The worker claims the head almost immediately, so reading /running can be a
# race: the item may already have moved from "pending" to "running". Queue a
# second identical-shape message and read the snapshot while the FIRST one is
# still occupying the worker -- then the second is guaranteed to be pending.
if mine is None:
    status, res2 = json_call(
        "POST",
        f"/api/sessions/{sid}/messages",
        {
            "content": "look at this",
            "images": ["data:image/png;base64," + __import__("base64").b64encode(PNG).decode()],
            "imageUrls": [file_url],
            "imageNames": ["queued-shot.png"],
        },
    )
    # Re-read: now the worker is busy with the first, so this one is pending.
    status, run = json_call("GET", f"/api/sessions/{sid}/running")
    items = run.get("queued", [])
    print(f"\nsecond submit -> {status}, pending={[q.get('content') for q in items]}")
    mine = next((q for q in items if q.get("content") == "look at this"), None)

check("queued item present in snapshot", mine is not None)
if mine:
    atts = mine.get("attachments") or []
    check("snapshot carries an attachment", len(atts) >= 1, f"count={len(atts)}")
    if atts:
        f = (atts[0] or {}).get("file") or {}
        check("attachment has a url", bool(f.get("url")), f"{f}")
        check("attachment url matches the upload", f.get("url") == file_url, f"{f.get('url')} vs {file_url}")
        check("attachment keeps the original name", f.get("name") == "queued-shot.png", f"{f.get('name')}")
        check("attachment has an image mime", str(f.get("mime_type", "")).startswith("image/"), f"{f.get('mime_type')}")
        # inline base64 must NOT ride along -- the whole point of the slim form.
        check("no inline base64 leaked into the snapshot",
              not str(f.get("contents", "")).startswith("data:"), f"contents={str(f.get('contents'))[:40]!r}")

print()
if failures:
    print(f"{len(failures)} check(s) FAILED: {failures}")
    sys.exit(1)
print("all checks passed")

#!/usr/bin/env python3
"""One-shot auth bootstrap for the isolated verification server.

Creates the password (so the server stops returning 401) and prints the
resulting bearer token, which is what every authenticated route expects.
"""
import json
import sys
import urllib.error
import urllib.request

BASE = sys.argv[1].rstrip("/")
PASSWORD = "verify-password-123"


def call(method, path, body=None, token=None):
    headers = {"Accept": "application/json"}
    if token:
        headers["Authorization"] = f"Bearer {token}"
    data = None
    if body is not None:
        data = json.dumps(body).encode()
        headers["Content-Type"] = "application/json"
    req = urllib.request.Request(f"{BASE}{path}", data=data, headers=headers, method=method)
    try:
        with urllib.request.urlopen(req, timeout=20) as r:
            raw = r.read().decode()
            return r.status, (json.loads(raw) if raw.strip() else {})
    except urllib.error.HTTPError as e:
        raw = e.read().decode()
        try:
            return e.code, json.loads(raw)
        except Exception:
            return e.code, {"raw": raw}


status, res = call("POST", "/api/auth/setup", {"password": PASSWORD})
print(f"setup -> {status} {res}")

status, res = call("POST", "/api/auth/login", {"password": PASSWORD})
print(f"login -> {status}")

tok = res.get("token")
if not tok:
    print("no token returned")
    sys.exit(1)

# Prove the token actually works on an authenticated route.
status, res = call("GET", "/api/sessions", token=tok)
print(f"authorized GET /api/sessions -> {status}")

with open(".tmp-verify/token.txt", "w", encoding="utf-8") as fh:
    fh.write(tok)
print("token written")

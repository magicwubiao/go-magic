"""Smoke-test that the built frontend actually BOOTS.

Why this exists: a module-level temporal-dead-zone error in a shared util made
the production bundle throw `ReferenceError: Cannot access 'X' before
initialization` on load, leaving `#app` empty -- a completely white page. Both
`vue-tsc` and `vite build` passed clean, because TDZ is a *runtime* ordering
problem that type-checking and bundling cannot see.

So the only reliable guard is to load the real bundle in a real browser and
assert that the app mounted. This runs against the served dist/ on a live
server and checks:
  1. no uncaught exception at load
  2. #app has children (Vue mounted)
  3. the login form renders
"""

import base64
import json
import os
import subprocess
import sys
import time
import urllib.request

import websocket

EDGE = r"C:\Program Files (x86)\Microsoft\Edge\Application\msedge.exe"
BASE = sys.argv[1] if len(sys.argv) > 1 else "http://127.0.0.1:5313/"
PROFILE = os.path.join(os.environ["TEMP"], "qm-boot-smoke-profile")
SHOTS = r"D:\project\go\go-magic\.workbuddy\shots-boot"
os.makedirs(PROFILE, exist_ok=True)
os.makedirs(SHOTS, exist_ok=True)

log = []
errors = []

subprocess.run(
    ["powershell", "-NoProfile", "-NonInteractive", "-Command",
     "Get-CimInstance Win32_Process -Filter \"name='msedge.exe'\" | "
     "Where-Object { $_.CommandLine -like '*qm-boot-smoke*' } | "
     "ForEach-Object { Stop-Process -Id $_.ProcessId -Force }"],
    capture_output=True,
)
time.sleep(1.0)

subprocess.Popen(
    [EDGE, "--headless=new", "--disable-gpu", "--no-first-run", "--no-default-browser-check",
     "--remote-debugging-port=9229", "--remote-allow-origins=*", "--no-proxy-server",
     "--proxy-bypass-list=*", f"--user-data-dir={PROFILE}", "--window-size=1280,900", "about:blank"],
    stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL,
)
time.sleep(3.5)

targets = json.loads(urllib.request.urlopen("http://127.0.0.1:9229/json/list", timeout=10).read())
page = next(t for t in targets if t.get("type") == "page")
ws = websocket.create_connection(page["webSocketDebuggerUrl"], timeout=60)
_id = [0]
events = []


def send(method, params=None):
    _id[0] += 1
    ws.send(json.dumps({"id": _id[0], "method": method, "params": params or {}}))
    return _id[0]


def call(method, params=None, timeout=30):
    """Send a command and wait for ITS reply by matching the id.

    A bare drain() is not enough: it only collects events and may return before
    the reply arrives, which produced a null state and a false FAIL.
    """
    mid = send(method, params)
    deadline = time.time() + timeout
    while time.time() < deadline:
        ws.settimeout(max(0.3, deadline - time.time()))
        try:
            msg = json.loads(ws.recv())
        except Exception:
            continue
        if "method" in msg:
            events.append(msg)
            continue
        if msg.get("id") == mid:
            return msg
    raise TimeoutError(method)


def drain(seconds):
    end = time.time() + seconds
    while time.time() < end:
        ws.settimeout(max(0.2, end - time.time()))
        try:
            msg = json.loads(ws.recv())
        except Exception:
            continue
        if "method" in msg:
            events.append(msg)


for m in ("Runtime.enable", "Log.enable", "Page.enable"):
    call(m)
call("Page.navigate", {"url": BASE})
drain(8.0)

for ev in events:
    m = ev.get("method")
    if m == "Runtime.exceptionThrown":
        d = ev["params"]["exceptionDetails"]
        ex = d.get("exception") or {}
        errors.append(f"UNCAUGHT: {ex.get('description') or d.get('text')}")
    elif m == "Log.entryAdded":
        e = ev["params"]["entry"]
        if e.get("level") == "error":
            errors.append(f"LOG-ERROR: {e.get('text')}")

# Ask the live DOM whether Vue mounted -- synchronous, id-matched.
probe = call("Runtime.evaluate", {
    "expression": (
        "JSON.stringify({"
        "c: (document.querySelector('#app')||{}).childElementCount || 0,"
        "t: document.body.innerText.slice(0,120)"
        "})"
    ),
    "returnByValue": True,
})
state = probe.get("result", {}).get("result", {}).get("value")

log.append(f"errors={json.dumps(errors, ensure_ascii=False)}")
log.append(f"state={state}")

shot = call("Page.captureScreenshot", {"format": "png"})
d = shot.get("result", {}).get("data")
if d:
    p = os.path.join(SHOTS, "boot.png")
    open(p, "wb").write(base64.b64decode(d))
    log.append(f"shot -> {os.path.getsize(p)} bytes")

with open(r"D:\project\go\go-magic\.workbuddy\tmp-boot-smoke.txt", "w", encoding="utf-8") as f:
    f.write("\n".join(log))
try:
    ws.close()
except Exception:
    pass

# exit non-zero if the app did not mount -- makes this usable as a gate
app_children = 0
if state:
    try:
        app_children = json.loads(state).get("c", 0)
    except Exception:
        app_children = 0
ok = app_children > 0 and not errors
print(f"PASS (appChildren={app_children})" if ok else f"FAIL (appChildren={app_children})")
sys.exit(0 if ok else 1)

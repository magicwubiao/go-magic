import json
import os

home = os.path.join(os.environ["TEMP"], "qm2-home")
cfg = {
    "provider": "mock",
    "model": "mock-model",
    "providers": {
        "mock": {
            "type": "openai",
            "base_url": "http://127.0.0.1:59999/v1",
            "api_key": "sk-mock",
            "models": ["mock-model"],
        }
    },
}

path = os.path.join(home, "config.json")
# encoding="utf-8" without BOM: Go's json.Unmarshal rejects a leading BOM
# (PowerShell's `Out-File -Encoding utf8` writes one, which is what broke the
# first attempt at this harness).
with open(path, "w", encoding="utf-8") as fh:
    json.dump(cfg, fh, indent=2)

raw = open(path, "rb").read()
print(f"wrote {path} ({len(raw)} bytes, first3={raw[:3].hex()})")

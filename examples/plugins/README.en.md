# Plugin Examples (Agent Plugins)

Every subdirectory here is a complete, installable Agent Plugin covering one component type of the plugin system. Start from the closest example and modify it instead of reading the spec from scratch.

| Directory | Demonstrates | External deps |
|---|---|---|
| [01-skill-only](01-skill-only/) | Skills only: SKILL.md frontmatter, per-skill scripts, icon, `examples` prompts | none |
| [02-mcp-stdio-notes](02-mcp-stdio-notes/) | A runnable stdio MCP server (Python stdlib only), `${PLUGIN_ROOT}` / `${PLUGIN_DATA}` expansion, data persistence | Python 3.8+ |
| [03-cli-gh](03-cli-gh/) | cli.json connector: `{{param}}` templates, `{{?param}}` conditionals, stdin params, credential placeholders, raw escape hatch | local `gh` binary |
| [04-mcp-http-remote](04-mcp-http-remote/) | Remote MCP: streamable-http and sse transports, `${CRED:NAME}` header credentials, OAuth 2.1 | network |
| [05-media-provider](05-media-provider/) | Extension namespace: contribute a backend to the built-in `image_gen` tool, return `b64` payloads | Python 3.8+ |
| [06-mcp-blender](06-mcp-blender/) | Local bridge: Blender. The server only relays — the add-on inside Blender listens on 9876 and the server connects back to it | Blender + add-on locally, uv/uvx |
| [07-mcp-unity](07-mcp-unity/) | Local bridge: Unity. The server comes from PyPI (`mcpforunityserver`); the Editor side is the MCP for Unity package in your project | Unity Editor + package in project, uv/uvx |
| [08-mcp-godot](08-mcp-godot/) | Local bridge: Godot. No add-on — it drives the Godot CLI directly. Shows an empty `GODOT_PATH` credential slot | Godot 4.x locally, Node/npx |
| [09-mcp-figma-desktop](09-mcp-figma-desktop/) | Local bridge: Figma desktop MCP on `127.0.0.1:3845`, no token — reads the selection in Dev Mode | Figma desktop app with Dev Mode |
| [10-mcp-jsdesign](10-mcp-jsdesign/) | Local bridge: JsDesign canvas bridge — a stdio process talks over WebSocket to the MCP Bridge plugin inside the client | JsDesign client + Bridge plugin, Node/npx |
| [11-mcp-freecad](11-mcp-freecad/) | Local bridge: FreeCAD. The add-on listens on `127.0.0.1:9875` over XML-RPC and the server only relays (9875 is not an MCP endpoint); includes token/tunnel notes for remote access | FreeCAD 1.1+ + add-on locally, uv/uvx |

> 06–11 are deliberately **not** built-in catalog entries: they only work when the host application runs on the same machine as go-magic. The built-in catalog has to stay usable on server deployments, so shipping these there would just add entries that always fail. Install the one you need — one app per example, no coupling.

## Install

```bash
magic plugin install ./examples/plugins/02-mcp-stdio-notes
magic plugin list
magic plugin remove notes-mcp-demo
```

The Web UI (Plugins page) does the same: upload a zip, enable/disable, uninstall, fill credentials, reconnect.

## Minimal skeleton

```
my-plugin/
  plugin.json
  skills/my-skill/SKILL.md
```

```json
{
  "$schema": "https://agent-plugins.org/schemas/1.0.0/plugin.schema.json",
  "name": "my-plugin",
  "version": "0.1.0",
  "description": "One sentence on what problem it solves"
}
```

`$schema` is mandatory — a missing or unsupported schema rejects the whole plugin. Everything else fails in isolation: a broken skill is skipped, a broken MCP entry is flagged, a broken `cli.json` only disables the CLI component.

Key conventions: empty env values and `${CRED:NAME}` placeholders become credential form fields (stored encrypted in `plugin_secrets/`); `${PLUGIN_ROOT}` / `${PLUGIN_DATA}` expand once in stdio `args` / `env` values / `cwd`; tool names are prefixed `ap_<plugin>_...`; plugin MCP and CLI tools always require per-call approval.

Full field reference: `docs/USAGE.en.md` section 15.4. `internal/agentplugin/examples_test.go` validates every example in this directory on each `go test ./internal/agentplugin/` run.

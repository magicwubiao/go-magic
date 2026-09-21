# go-magic Usage Guide

> Magic Agent — a high-performance AI agent framework written in Go, with a built-in TUI, Web Dashboard, Bot mode, and messaging gateways.
>
> Version: v0.5.9 ｜ This document is based on the real command tree of compiled binaries (34 top-level commands, 164 sub-commands in total).

---

## Table of Contents

- [1. What it does](#1-what-it-does)
- [2. Installation](#2-installation)
- [3. Quick Start (up and running in 5 minutes)](#3-quick-start-up-and-running-in-5-minutes)
- [4. Directory Layout & Data Storage](#4-directory-layout--data-storage)
- [5. Configuration Reference](#5-configuration-reference)
- [6. CLI Overview](#6-cli-overview)
- [7. Core Commands](#7-core-commands)
- [8. TUI Slash Commands](#8-tui-slash-commands)
- [9. Web Dashboard](#9-web-dashboard)
- [10. Bot Mode (Named Agents)](#10-bot-mode-named-agents)
- [11. Group Chat & Rooms](#11-group-chat--rooms)
- [12. Messaging Gateways](#12-messaging-gateways)
- [13. Skills System](#13-skills-system)
- [14. Tools & Toolsets](#14-tools--toolsets)
- [15. MCP & ACP](#15-mcp--acp)
- [16. Scheduled Tasks (Cron)](#16-scheduled-tasks-cron)
- [17. Kanban Board](#17-kanban-board)
- [18. Token Usage & Budget](#18-token-usage--budget)
- [19. Privacy Redaction](#19-privacy-redaction)
- [20. Operations: Diagnostics, Backup, Update](#20-operations-diagnostics-backup-update)
- [21. FAQ](#21-faq)
- [22. Known Issues (verified)](#22-known-issues-verified)

---

## 1. What it does

| Capability | Description |
|------------|-------------|
| **Multi-Provider** | 22 built-in providers: openai, anthropic, deepseek, minimax, ollama, dashscope, vllm, zhipu, openrouter, gemini, groq, together, mistral, cohere, perplexity, huoshan, wenxin, moonshot, mimo, hunyuan, longcat, meta; any OpenAI-compatible endpoint via the `custom` provider |
| **Multiple models per provider** | Each provider can have several models; **the first item in the array is the active model**, switchable on the fly without restart |
| **TUI chat** | Built on BubbleTea — streaming output, Markdown rendering, multi-line input, slash commands |
| **Web Dashboard** | Vue 3 + TypeScript, 16 functional pages, streaming chat, hot-reload config, file management, usage panel, and more |
| **Bot mode** | Named agent profiles: independent persona, model binding, persistent sessions, scheduled routines; bots can message each other |
| **Group chat / Rooms** | Multiple bots hold multi-round conversations in one room and @-mention each other to pull others in |
| **Cross-machine Peer** | A bot on one machine can message a bot on another machine directly |
| **Messaging gateways** | 14 platforms: telegram, discord, slack, wechat_ilink, wecom, dingtalk, feishu, qq, line, matrix, teams, googlechat, email, sms |
| **Tool system** | 49 built-in tools, 12 toolsets (web / file / terminal / browser / code_execution / memory / skills / session / cron / delegation / utility / homeassistant) |
| **Skills system** | Progressive loading (L0/L1/L2), three-tier sources (built-in / global / workspace), with a review flow for auto-generated skills |
| **Cortex cognitive architecture** | Memory (SOUL.md / USER.md / snapshots), perception, planning, execution, skill evolution |
| **Usage tracking** | Token and cost stats by model / session / day, with monthly budget alerts |
| **Privacy redaction** | Auto-masks phone numbers, emails, ID cards, bank cards, IPs, and addresses in logs and output |

---

## 2. Installation

### 2.1 Download prebuilt binaries (recommended)

```bash
# Linux / macOS — the release assets are plain binaries (no .tar.gz, no extraction)
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/')
curl -fL "https://github.com/magicwubiao/go-magic/releases/latest/download/go-magic-${OS}-${ARCH}" -o magic
chmod +x magic
sudo mv magic /usr/local/bin/magic
```

Windows users: download `go-magic-windows-amd64.exe` (or `go-magic-windows-arm64.exe` on ARM64)
directly from [Releases](https://github.com/magicwubiao/go-magic/releases).


### 2.2 Go Install

```bash
go install github.com/magicwubiao/go-magic/cmd/magic@latest
```

### 2.3 One-line script (Linux / macOS)

```bash
curl -fsSL https://raw.githubusercontent.com/magicwubiao/go-magic/main/scripts/install.sh | bash
```

Homebrew / Scoop install scripts are also available under `scripts/` (they require the
corresponding tap / bucket repositories to be published first; otherwise use `install.sh` above).

### 2.4 Docker

```bash
# Quick run (map 8642 so the Web UI is reachable from the host)
docker run -it -p 8642:8642 magicwubiao/go-magic

# Or with compose
docker compose up -d
```

> ℹ️ **About the port**: `magic server`'s default port is **5000**, but the image's `CMD` is fixed at `["server", "--port", "8642"]`, matching `EXPOSE 8642` and compose's `8642:8642`. So running the image as-is works — you do **not** need to append `server --port 8642`:
> ```bash
> docker run -p 8642:8642 magicwubiao/go-magic
> ```
> Only when you **override the container command** (e.g. `docker run ... magicwubiao/go-magic server` — positional args replace `CMD` while `ENTRYPOINT` stays `/app/magic`) must you pass `--port 8642` yourself, otherwise the container falls back to listening on 5000 and the mapping breaks.
> See [22. Known Issues → 22.2](#22-known-issues-verified).

### 2.5 Build from source

Requirements: **Go 1.26+**, **Node.js 20+** (for the Web Dashboard).

```bash
git clone https://github.com/magicwubiao/go-magic.git
cd go-magic

make build        # build frontend + CLI
make build-all    # common platforms (Linux / macOS / Windows)
make build-cross  # all supported platforms

# specific platform
make build-linux
make build-macos
make build-windows
```

Common Make targets:

| Target | Purpose |
|--------|---------|
| `make build` | build-web + build-cli |
| `make build-web` | build frontend only (vite output goes to `internal/server/dist`, embedded by Go) |
| `make build-cli` | build the Go binary only |
| `make test` / `make test-short` / `make test-coverage` | tests |
| `make lint` | lint |
| `make run` / `make run-server` | run TUI / Dashboard |
| `make docker` / `make docker-run` / `make docker-stop` | image related |
| `make clean` | clean artifacts |

---

## 3. Quick Start (up and running in 5 minutes)

```bash
# 1. Interactive init: choose provider, enter API key, pick model, configure tools
magic setup

# 2. Start chatting (TUI)
magic chat

# 3. Or launch the Web Dashboard
magic server
# open http://localhost:5000 in your browser
```

Skip parts of the wizard:

```bash
magic setup --skip-model      # skip model selection
magic setup --skip-tools      # skip tool configuration
magic setup --skip-gateway    # skip gateway configuration
```

You can also configure everything via the CLI and skip the wizard entirely:

```bash
magic config set providers.deepseek.api_key sk-xxxxxxxx
magic config set provider deepseek
magic model deepseek:deepseek-chat
magic doctor                # self-check
```

---

## 4. Directory Layout & Data Storage

All data is stored under **magic home**, which is `~/.magic` by default (Windows: `%USERPROFILE%\.magic`).

```
~/.magic/
├── config.json          main config file
├── sessions.db          session database (SQLite, with FTS5 full-text index)
├── bots.db              bot-mode database
├── kanban.db            kanban database
├── instance_id          this machine's identity (used for cross-machine peers, lazily generated)
├── peers.json           known peer list (permission 0600)
├── .auth_token          dashboard login token
├── bots/                one <name>.json per bot; private vars in <name>/.env
├── skills/              global skills
├── builtin_skills/      built-in skill copies
├── plugins/             plugins
├── logs/                logs, named magic_YYYY-MM-DD_HH-MM-SS.log
├── usage/               usage stats
├── cron/                cron job data
├── cortex/              Cortex cognitive data
├── memories/            memory data
└── workspace/           workspace files
```

Switch magic home:

```bash
magic --magic-home /data/magic config list
export GO_MAGIC_HOME=/data/magic     # env var form (this is what the code actually reads)
```

---

## 5. Configuration Reference

### 5.1 Config file location

```bash
magic config path      # print the current config file path
magic config list      # list all config
magic config get <key> # read a single value
magic config set <key> <value>
magic config validate  # validate
magic config reset     # restore defaults
```

### 5.2 Full example

```jsonc
{
  "profile": "default",                 // profile name
  "magic_home": "~/.magic",
  "provider": "deepseek",               // current provider
  "model": "deepseek-chat",             // deprecated; prefer providers.<x>.models[0]
  "chat_mode": "chat",                  // default mode for magic chat: chat | coding
  "browser_profile_dir": "~/.magic/browser-profile",  // persistent browser profile dir (this is the default when omitted); a leading ~ expands to the user home dir; set it to "" for a fresh temp profile each start (no cookies/login)
  "browser_headless": true,             // run the browser headless (this is the default when omitted); keep true on servers/containers; false needs a display server (Xvfb/noVNC) and is only for manual login; the BROWSER_HEADLESS env var takes precedence

  "providers": {
    "deepseek": {
      "api_key": "sk-xxxx",
      "base_url": "https://api.deepseek.com/v1",   // optional
      "models": ["deepseek-chat", "deepseek-reasoner"]
    },
    "openai":    { "api_key": "sk-xxxx", "models": ["gpt-5.6", "gpt-5.6-terra"] },
    "anthropic": { "api_key": "sk-ant-xxxx", "models": ["claude-sonnet-5"] },
    "ollama":    { "base_url": "http://localhost:11434", "models": ["llama3.3", "qwen3"] }
  },

  "tools":  { "enabled": ["all"], "disabled": [] },
  "skills": { "enabled": [], "disabled": [], "user_dir": "" },

  "agent": {
    "max_turns": 300,          // tool-loop cap per turn, 0 = built-in default
    "max_iterations": 400,     // turn-control cap
    "max_token_budget": 0      // token budget, 0 = unlimited
  },

  "bot_mode": {
    "enabled": false,
    "inject_bot_protocol": true,   // runtime defaults to on when nil
    "history_window": 200,         // history messages kept per bot, 0 = 200
    "turn_timeout_minutes": 5,     // per-round timeout, 0 = 5 min
    "relay_token": ""              // shared secret for cross-machine relay; empty = anonymous (trusted networks only)
  },

  "gateway": {
    "enabled": false,
    "rate_limit_per_user": 20,     // messages per user per minute, 0 = default 20, negative = unlimited
    "rate_limit_window_sec": 60,
    "blocked_users": [],
    "sensitive_words": [],
    "platforms": {
      "telegram": {
        "enabled": true,
        "token": "123456:ABC-DEF",
        "dm_policy": "open",            // open | allowlist | disabled
        "group_policy": "open",
        "allowed_channels": [],
        "blocked_channels": []
      }
    }
  },

  "memory": { "enabled": true },
  "cortex": { "enabled": true, "skill_min_pattern_freq": 3 },

  "mcp": { "servers": {} },
  "subagent": { "max_concurrent": 4, "max_depth": 2, "timeout": 120 },

  "approval": {
    "strategy": "smart",           // smart | manual | auto
    "trust_threshold": 3,
    "enable_learning": true,
    "enable_cli_confirm": false,
    "approval_timeout": 60,
    "timeout_strategy": "deny"     // deny | allow_low_medium | allow_all
  },

  "privacy": {
    "enabled": true,
    "redact_phone": true,
    "redact_email": true,
    "redact_id_card": true,
    "redact_bank_card": true,
    "redact_ip": true,
    "redact_address": true,
    "custom_patterns": {}
  },

  "display": { "skin": "default", "no_color": false, "show_banner": true },
  "server":  { "file_strategy": "auto", "upload_url_prefix": "" }
}
```

### 5.3 Environment variables

| Variable | Description | Notes |
|---------|-------------|-------|
| `GO_MAGIC_HOME` | override magic home directory (default `~/.magic`) | ✅ actually read by code |
| `GO_MAGIC_CORS_ORIGINS` | extra CORS origins allowed by the Dashboard, comma-separated | ✅ |
| `MAGIC_SKILL_DIR` | override the skills directory | ✅ |
| `MAGIC_SESSION_ID` | bind to a specific session ID | ✅ |
| `BROWSER_HEADLESS` | overrides the headless toggle; `false`/`0`/`no`/`off` turns it off, any other non-empty value turns it on. When unset, the config field `browser_headless` applies (default on) | ✅ |
| `BROWSER_PROFILE_DIR` | override the automation browser persistent profile directory; empty = fresh temp profile each run | ✅ |
| `CHROME_PATH` / `EDGE_PATH` | specify the Chrome / Edge executable path | ✅ |
| `OPENAI_API_KEY` / `DEEPSEEK_API_KEY` / `ANTHROPIC_API_KEY` / `GOOGLE_API_KEY` / `GROQ_API_KEY` / `OPENROUTER_API_KEY` / `TOGETHER_API_KEY` | per-provider API keys | ✅ |
| `TELEGRAM_BOT_TOKEN` / `DISCORD_BOT_TOKEN` | gateway tokens | ✅ |

> Switch profiles via the `--profile/-p` flag or the `profile` field in the config file — there is no corresponding environment variable.

### 5.4 Global flags

Available on all sub-commands:

| Flag | Description |
|------|-------------|
| `-v, --verbose` | verbose output |
| `--debug` | debug mode (implies verbose) |
| `--config <path>` | specify config file path |
| `-o, --output <fmt>` | output format: `text` / `json` / `yaml` / `table` |
| `--no-color` | disable colored output |
| `--magic-home <dir>` | specify magic home |
| `-p, --profile <name>` | specify profile |

> `-p` is a globally reserved shorthand. It conflicts with local flags of some sub-commands; see [22. Known Issues](#22-known-issues-verified).

---

## 6. CLI Overview

```
magic
├── chat                 TUI interactive chat
├── server               Web Dashboard
├── setup                interactive setup wizard
├── model                select/view provider and model
├── config               config management
│   ├── get / list / path / reset / set / validate
├── sessions             session management  list / show / rename / delete
├── skills               skill management  list / show / search / match / create / install /
│   │                              uninstall / import / delete / enable / disable
│   ├── auto              auto-generated skill review  list / pending / approve / reject /
│   │                                    archive / restore / delete / status / stats
│   ├── hub               remote skill hub  list / audit
│   └── config            skill config  list / disabled
├── tools                 tools  list
│   └── toolsets          toolsets  list / show / enable / disable
├── mcp                  MCP servers  list / add / connect / disconnect / health
├── acp                  ACP agent interop  serve / connect / disconnect / list / ping / skills / call
├── plugin                plugins  list / search / discover / install / uninstall /
│                               enable / disable / reload / update / info / check
├── agent                 sub-agents  spawn / list / kill / stats
├── cron                  scheduled tasks  list / add / edit / remove / toggle / test
├── kanban                kanban board  create / list / show / start / claim / complete / block /
│                              unblock / archive / comment / link / unlink / triage /
│                              board / stats / delete
├── bots                 Bot mode  create / list / show / edit / clone / chat /
│   │                             message / hide / unhide / remove
│   └── routine          bot routines  add / list / enable / disable / remove
├── rooms                 group-chat rooms  create / list / show / send / messages / remove
├── peer                  cross-machine nodes  add / list / dm / remove
├── gateway               messaging gateway  start / stop / restart / status / setup
├── privacy              PII redaction  detect / redact / audit / stats
├── skin                  themes  list / show / preview / create / set / export / delete
├── voice                 voice  listen / speak / test
├── vision                image understanding  analyze / compare
├── coding                coding assistant  init / analyze / lint / test / build / debug
├── docs                  doc generation  generate | serve | man
├── usage                token usage (default 1 day, -d to specify days)
├── logs                  logs  list / show / latest
├── status / health / metrics / stats / doctor   system status and diagnostics
├── backup                full backup
├── update                self-update
├── migrate               migrate config from OpenClaw
├── completion            generate shell completion scripts
└── validate / version
```

---

## 7. Core Commands

### 7.1 `magic chat` — TUI chat

```bash
magic chat                 # streaming output by default
magic chat --no-stream     # disable streaming
magic chat -v              # with verbose logs
```

Once inside, just type. Enter `/help` to see all slash commands.

**Chat mode vs Coding mode**

| Item | Chat (default) | Coding |
|------|---------------|--------|
| File write permission | restricted | relaxed |
| Command exec timeout | 30s | 300s |
| Python / Node code exec | disabled | enabled |
| Shell access | limited | full |
| Auto-approve tools | no | yes |

Inside the TUI use `/mode coding` to switch, `/mode chat` to switch back. Persist the default via config:

```bash
magic config set chat_mode coding
```

### 7.2 `magic server` — Web Dashboard

```bash
magic server                 # default http://localhost:5000
magic server --port 8642     # change port
magic server --open          # open browser after start
```

On startup it prints the database path and listen address; `Ctrl+C` shuts down gracefully.

### 7.3 `magic model` — switch provider / model

```bash
magic model                              # view current
magic model gpt-5.6                      # change model only (current provider)
magic model deepseek:deepseek-chat       # change both provider and model
magic model --list openai                # list available models of a provider
magic model huoshan:ep-20250105-xxxxx    # Volcengine Ark endpoint IDs also supported
```

### 7.4 `magic sessions` — session management

```bash
magic sessions list              # newest-first by last update
magic sessions show <id>
magic sessions rename <id> <new-name>
magic sessions delete <id>       # irreversible, prefer magic backup first
```

### 7.5 `magic agent` — parallel sub-agents

```bash
magic agent spawn "code review" "review ./internal/server for concurrency safety" \
    --tools read_file,search_in_files --timeout 300 --max-depth 2
magic agent list
magic agent stats
magic agent kill <agent-id>
```

### 7.6 `magic coding` — coding assistant

```bash
magic coding init --type go            # detect/init project (go/python/node/rust/java/cpp)
magic coding analyze ./main.go --ai    # AI deep analysis, -f for fix suggestions
magic coding lint ./src --fix --format json
magic coding build --target linux/amd64 --release -o myapp
magic coding debug --chat              # interactive debug with coding context
```

---

## 8. TUI Slash Commands

Used inside `magic chat`:

| Command | Alias | Description |
|---------|-------|-------------|
| `/help [command]` | `/?` | help |
| `/commands [category]` | `/cmds` | list all commands |
| `/new` | | start a new conversation |
| `/clear` | | clear chat history |
| `/compress` | | compress context window |
| `/retry` | | retry last response |
| `/undo` | | undo last action |
| `/export [format]` | `/save` | export conversation |
| `/handoff [model]` | | hand the session off to another model |
| `/model [provider:model]` | `/m` | switch model |
| `/mode [chat\|coding]` | | switch mode |
| `/personality [name]` | `/persona`, `/tone` | set persona |
| `/tools [category]` | | list available tools |
| `/skills [name]` | `/skill` | list available skills |
| `/context [add\|remove\|list]` | `/ctx` | manage context files |
| `/clarify [question]` | | ask the user for clarification |
| `/interrupt [reason]` | | interrupt execution |
| `/subgoal <text>` | | add a subgoal to the current goal |
| `/goals` | | goal management |
| `/status` | | system status |
| `/version` | `/ver` | version |
| `/usage [--days N]` | | token usage |
| `/insights [-d N]` | | usage insights (default 7 days) |
| `/sessions [list\|search]` | `/session` | session list |
| `/sethome [session_id]` | | set the gateway's home session |
| `/stop` | `/cancel` | stop current operation |

---

## 9. Web Dashboard

Launch: `magic server` (default `http://localhost:5000`). On first visit it walks you through setting a login password (**at least 8 characters**, stored with bcrypt). Once set, all endpoints except `/api/health`, `/api/status`, `/metrics`, `/api/fs/shared/*`, and `/api/relay/v1/dm` require an authenticated session.

### Page list

| Route | Page | Description |
|-------|------|-------------|
| `/chat` | Chat | streaming chat, session management |
| `/kanban` | Kanban | multi-agent task collaboration |
| `/config` | Config | edit config online, hot reload |
| `/models-providers` | Models & Providers | multi-model management, instant switching |
| `/tools` | Tools | tool / toolset toggles |
| `/skills` | Skills | browse, install, enable/disable, review auto-generated skills |
| `/cron` | Cron | cron job management |
| `/gateway` | Gateway | platform start/stop, QR-code login |
| `/bots` | Bots | bot profile management |
| `/logs` | Logs | live logs |
| `/system` | System | system info and health checks |
| `/profiles` | Profiles | switch between multiple profiles |
| `/goals` | Goals | goal decomposition and tracking |
| `/approval` | Approval | approve high-risk tool calls |
| `/files` | Files | browse / upload / download / archive online |
| `/usage` | Usage | token & cost stats, budget progress |
| `/mcp` | MCP | MCP server management |
| `/plugins` | Plugins | agent plugin management |
| `/login` | Login | public route |

### Key APIs (called same-origin from the frontend)

```
/api/auth/*           login and init
/api/chat             non-streaming chat
/api/chat/stream      streaming chat (SSE)
/api/sessions/*       session CRUD and search
/api/models/*         model query and switch (hot reload)
/api/providers/*     provider management
/api/config/*         config read/write
/api/skills/*         skills
/api/tools/*          tools and toolsets
/api/mcp/servers/*   MCP
/api/cron/jobs/*      scheduled jobs
/api/kanban/*         kanban
/api/bots/*          bots
/api/fs/*             filesystem
/api/usage/*          usage and budget
/api/gateway/*        gateway control
/api/logs/*           logs
/api/relay/v1/dm      cross-machine relay (no login check; token validated separately in the body)
/metrics             Prometheus metrics
```

---

## 10. Bot Mode (Named Agents)

A Bot is a **named configuration profile**: role tag, persona prompt, provider/model binding, tool & skill allowlist, private env vars, independent persistent sessions, and scheduled routines.

### 10.1 Enable

```bash
magic config set bot_mode.enabled true
```

### 10.2 Create & manage

```bash
magic bots create researcher \
  --title "Research Assistant" \
  --desc "Responsible for retrieving and summarizing papers" \
  --prompt "You find and summarize papers; output must include citation links." \
  --provider deepseek --model deepseek-chat \
  --tools web_search,web_fetch,read_file \
  --skills "paper-review" \
  --env "SERPAPI_API_KEY=xxxx" \
  --avatar "🔬" \
  --memory "User preference: Chinese output, conclusions first"

magic bots list           # list all bots and their status
magic bots show researcher
magic bots edit researcher --model deepseek-reasoner
magic bots clone researcher analyst     # copy config, clear session history
magic bots hide researcher / unhide researcher   # show/hide in the Dashboard list
magic bots remove researcher
```

`--env` is written to `~/.magic/bots/<name>/.env`, giving **per-bot credentials**.

### 10.3 Conversation

```bash
magic bots chat researcher "find the latest papers on agent memory"
magic bots message researcher coder "turn this summary into a Markdown table"   # bot → bot, fire-and-forget
```

### 10.4 Scheduled routines

```bash
magic bots routine add researcher daily-digest \
  --schedule "0 9 * * *" \
  --prompt "Summarize yesterday's findings."

magic bots routine list researcher
magic bots routine enable  researcher daily-digest
magic bots routine disable researcher daily-digest
magic bots routine remove  researcher daily-digest
```

### 10.5 Use bots on chat platforms

On any connected gateway platform:

```
/bot researcher find the latest papers on agent memory
@researcher find the latest papers on agent memory
```

### 10.6 Cross-machine Peer

A bot on one machine can message a bot on another machine directly (HTTP relay).

```bash
# Machine B (callee): make sure dashboard or gateway is running; relay endpoint uses the service port
magic config set bot_mode.relay_token "shared-secret"    # strongly recommended

# Machine A (caller)
magic peer add lab-b http://192.168.1.20:5000 --token shared-secret
magic peer list
magic peer dm lab-b researcher "how is the report going?"
magic peer remove lab-b
```

- Peer table: `<magicHome>/peers.json` (permission 0600)
- This machine's identity: `<magicHome>/instance_id`
- Relay endpoint: `POST http://<host>:<port>/api/relay/v1/dm`, messages prefixed with `[relay from@instance]`
- **DM is blocking**: it waits until the remote bot finishes the whole round, so a slow remote = a slow command (client timeout 6 min, response cap 4MB)
- With an empty `relay_token`, relay accepts anonymous requests (trusted networks only)

---

## 11. Rooms

A Room holds **2–6 bots**. After a human sends a message, members speak in turns (up to 3 rounds) and pull each other in with `@name`. A bot starting its reply with `@user` interrupts the current round and throws the question back to the human.

```bash
magic rooms create design-review --members researcher,coder --topic "architecture review"
magic rooms send <room-id> "Review the plan" --target researcher
magic rooms messages <room-id>
magic rooms show <room-id>
magic rooms list
magic rooms remove <room-id>
```

---

## 12. Messaging Gateways

Gateways connect the agent to external chat apps; the health-check endpoint is `http://localhost:8081/health`.

```bash
magic gateway setup      # interactive platform config
magic gateway start                       # start all enabled platforms
magic gateway start -P telegram           # start a single platform
magic gateway status
magic gateway stop
magic gateway restart
```

### 12.1 Supported platforms

| Platform ID | Name | Key config |
|-------------|------|------------|
| `telegram` | Telegram | `token` |
| `discord` | Discord | `token` |
| `slack` | Slack | `bot_token` (or `token`), `signing_secret` (or `app_secret`) |
| `wechat_ilink` | WeChat iLink | `token`, `base_url`, `data_dir` |
| `wecom` | WeCom (official AI Bot, scan to create) | `bot_id`, `secret` (WebSocket, no public callback URL) |
| `dingtalk` | DingTalk | `app_key`, `app_secret` |
| `feishu` | Feishu / Lark | `app_id`, `app_secret` |
| `qq` | QQ (official bot only) | `app_id`, `app_secret` |
| `line` | LINE | `channel_token` (or `token`), `channel_secret` (or `secret`) |
| `matrix` | Matrix | `homeserver`, `user_id`, `token` (or `mode: password` + `password`) |
| `teams` | Microsoft Teams (Bot Framework) | `app_id`, `app_secret`; Messaging endpoint → public URL + `/teams/events` (callback port 8088) |
| `googlechat` | Google Chat | `webhook_url`, `events_token`; Events delivery → `/gchat/events` (callback port 8089) |
| `email` | Email (IMAP receive + SMTP send) | `email`, `imap_host`/`imap_port`/`imap_user`/`imap_pass`, `smtp_host`/`smtp_port`/`smtp_user`/`smtp_pass`, `poll_interval` (polls INBOX — no callback port) |
| `sms` | SMS (Twilio) | `account_sid`, `auth_token`, `from`; Messaging webhook → `/sms/events` (callback port 8090) |

### 12.2 Config example

```jsonc
"gateway": {
  "enabled": true,
  "rate_limit_per_user": 20,
  "blocked_users": ["spam-user-id"],
  "sensitive_words": [],
  "platforms": {
    "telegram": {
      "enabled": true,
      "token": "123456:ABC-DEF",
      "dm_policy": "allowlist",              // open | allowlist | disabled
      "dm_allowlist": ["111222333"],
      "group_policy": "open",
      "allowed_channels": ["-1001234567890"],// allowlist; empty = all
      "blocked_channels": [],
      "mention_patterns": []                 // extra regexes treated as @
    },
    "wecom": {
      "enabled": true,
      "mode": "aibot",            // official AI Bot only (scan to create in Web UI)
      "bot_id": "bot_xxxx",
      "secret": "sec_xxxx"
    }
  }
}
```

### 12.3 Talking to bots on platforms

```
/bot <name> <message>
@<tag> <message>
```

Use `/sethome <session_id>` to set a session as the gateway's "home session".

---

## 13. Skills System

Skills load from three tiers; on name collision the latter wins:

1. **Built-in skills** — bundled with the program
2. **Global skills** — `~/.magic/skills/`
3. **Workspace skills** — `./skills/` or `.magic/skills/`

Supported formats: `SKILL.md` (with YAML frontmatter, recommended), `.json`, `.md`/`.markdown`, `.txt`, or a directory with `manifest.json`.

```bash
magic skills list                        # table listing
magic skills list --format json          # table | list | json
magic skills list --source global        # builtin | global | local | imported
magic skills list --filter api --show-tools
magic skills show <name>
magic skills search <keyword>
magic skills match "review this code for me"   # see which skills would trigger
magic skills create <name> [--force]
magic skills install <name> --from ./path-or-url
magic skills uninstall <name>
magic skills enable <name> / disable <name>
magic skills delete <name>
```

### 13.1 Import existing skills

```bash
magic skills import ./path/to/skill
magic skills import ./skills --recursive --force
magic skills import ./skills --list       # preview only, no import
```

Supports three sources: OpenClaw (`trigger_conditions`), Hermes (`hermes` metadata), and generic SKILL.md.

### 13.2 Auto-generated skill review flow

Cortex derives new skills from your usage patterns, but **does not activate them automatically** — human review is required:

```bash
magic skills auto list       # all and their status
magic skills auto pending    # pending review
magic skills auto approve <name>    # approve → then injected into the agent prompt
magic skills auto reject <name>
magic skills auto archive <name>    # archive (file kept, no longer active)
magic skills auto restore <name>
magic skills auto delete <name>
magic skills auto status <name>
magic skills auto stats      # counts per status
```

State flow: `pending` → `approved` ⇄ `archived`, `rejected` pending deletion.

### 13.3 Skill hub

```bash
magic skills hub list
magic skills hub audit       # install audit log
magic skills config list
magic skills config disabled
```

---

## 14. Tools & Toolsets

```bash
magic tools list             # list all tools (currently 49)
magic tools list --json
magic tools toolsets list    # 12 toolsets
magic tools toolsets show <name>
magic tools toolsets enable <name> / disable <name>
```

### Toolset ↔ tool mapping

| Toolset | Tools |
|---------|-------|
| **web** | `web_search`, `web_fetch` |
| **file** | `read_file`, `write_file`, `file_edit`, `list_files`, `search_in_files` |
| **terminal** | `execute_command`, `terminal`, `process` |
| **browser** | `browser_navigate`, `browser_snapshot`, `browser_click`, `browser_type`, `browser_scroll`, `browser_back`, `browser_get_images`, `browser_console` |
| **code_execution** | `execute_code` (Python / Node.js) |
| **memory** | `memory_store`, `memory_recall` |
| **skills** | `skill_list`, `skill_view`, `skill_manage`, `skill_create`, `skill_delete` |
| **session** | `session_search` |
| **delegation** | `delegate_task`, `poll_task`, `list_tasks`, `cancel_task` |
| **cron** | `cronjob` |
| **homeassistant** | `ha_list_entities`, `ha_get_state`, `ha_list_services`, `ha_call_service`, `ha_events`, `ha_config` |
| **utility** | `json`, `yaml`, `string`, `hash`, `uuid`, `random`, `time`, `math`, `csv`, `env`, `system_info` |

Toggle via config:

```jsonc
"tools": { "enabled": ["all"], "disabled": ["browser_*"] }
```

### 14.1 Running the automation browser on a headless server

The `browser_*` tools do **not** fetch pages over plain HTTP — they drive a real
Chromium through CDP using chromedp. So "my server has no browser" has two
distinct layers, and both need fixing:

**Layer 1: no Chromium binary.** Container images ship it (the Alpine `chromium`
package, see `Dockerfile`). On a bare-metal box install it yourself:

```bash
# Debian / Ubuntu
apt-get install -y chromium fonts-noto-cjk
# CentOS / RHEL / Rocky
dnf install -y chromium fonts-noto-cjk
```

Then make sure `magic` can find it (see the lookup order below), or point at it
explicitly with `CHROME_PATH`:

```bash
CHROME_PATH=/usr/bin/chromium magic server --port 8642
```

**Layer 2 (the one people miss): no X display.** Containers and most cloud hosts
have no display server, and Chrome tries to open a GUI window by default, dying
with `cannot open display`.

**Good news: the code already defaults to headless — there is nothing to
configure.** Precedence, highest first:

1. the `BROWSER_HEADLESS` environment variable (`false`/`0`/`no`/`off` turns it
   off; any other non-empty value turns it on)
2. the `browser_headless` config field
3. the built-in default, **`true` (headless)**

If you genuinely want a headed window (only useful for manual login), pick either:

```bash
export BROWSER_HEADLESS=false              # temporary
magic config set browser_headless false    # or persist it in the config
```

> **Why the default has to be headless**: the normal shape of a server is "no
> display service" — containers, cloud hosts, CI, systemd units all qualify.
> Running a headed Chrome there fails outright, and the agent cannot see why
> (it can only call tools), so the default has to be the safe one.
>
> **Headed without a display now fails loudly** instead of leaving you to guess
> at Chrome's obscure `cannot open display`: the error names the source of the
> `false` (environment variable or config file).

#### Headless does not mean the UI is gone — the browser screenshots itself

This is the most common misconception. Headless only means "no window on a
display"; the page still renders normally, so all of this keeps working:

every `browser_*` interaction (click / type / scroll / run JS / read the DOM),
and **`browser_vision`** (the server takes the screenshot and hands it to a
multimodal model).

In other words: **if the goal is "let the agent see and operate web pages", one
Chromium install is enough — headless is the default. No Xvfb, no VNC.**

#### When you actually need to *see* the UI

Exactly one scenario: **the first login, when a human has to scan a QR code, type
a password, or clear a CAPTCHA.** A headless Chrome has nowhere for a human to
click, so that is the only case that needs Xvfb + x11vnc + noVNC (or logging in
locally and importing the cookies into the server).

The persistent-profile mechanism exists precisely for this
(`browser_profile_dir`, default `~/.magic/browser-profile`): the logged-in state
lands on disk, so you log in **once** and the day-to-day runs are fully headless.

#### Option comparison

| Option | Needs an X server | Extra install | When to use |
|--------|------------------|---------------|-------------|
| **Chromium (headless by default)** | No | just chromium | **Recommended.** ~95% of cases: JS-rendered scraping, automation, screenshot understanding. **Zero configuration needed** |
| Chromium + Xvfb (no VNC) | Yes (virtual) | + xvfb, plus `BROWSER_HEADLESS=false` | Sites that detect a headless UA/fingerprint and refuse to serve. A "headed" Chrome under Xvfb evades some of that, but you still cannot see anything |
| **Chromium + Xvfb + noVNC** | Yes (virtual) | + xvfb/x11vnc/websockify | You must log in **manually on the server** (QR code, CAPTCHA). Open `http://<host>:6080` in your own browser to drive the container's Chrome |
| X11 forwarding / VNC to a real desktop | Yes (real) | full desktop stack — heavy and slow | Not recommended; installing a desktop just for a browser is a bad trade |
| Local Chrome + exposed CDP port | No (on the server) | networking + auth to sort out | Server only computes; the browser runs on your own machine. **Caveat**: `BrowserManager` currently only launches a local Chrome — there is **no** `CDP_URL`-style remote-attach config, so this needs a code change |

#### One-time manual login on the server (noVNC)

Use `docker compose` to start a second container that shares the same profile
volume, then remove it once you are logged in:

```yaml
# docker-compose.override.yml (only while logging in)
services:
  chrome-login:
    image: zenika/alpine-chrome:with-node   # or any image that has chromium
    command: >
      sh -c "Xvfb :99 -screen 0 1440x900x24 &
             x11vnc -display :99 -forever -nopw -listen 0.0.0.0 -rfbport 5900 &
             websockify --web /usr/share/novnc 6080 localhost:5900 &
             chromium --no-sandbox --user-data-dir=/profile --remote-debugging-port=9222"
    ports:
      - "6080:6080"        # open http://<server-ip>:6080/vnc.html
    volumes:
      - magic-browser-profile:/profile
```

When the login is done, run `docker compose rm -sf chrome-login` and the agent in
the main container can use that session immediately. **The login window only
exists while that container is running — delete it right after logging in, and
never leave 5900/6080 exposed to the public internet long-term.**

#### Verification checklist

```bash
# 1. can the container see chromium?
docker compose exec magic which chromium

# 2. is headless really on? (it defaults to on -- confirm nothing overrode it)
docker compose exec magic printenv BROWSER_HEADLESS   # empty = the default true applies

# 3. run browser_navigate once and look at the "method" field
#    "browser" = real browser worked; "http" = it silently fell back to a plain fetch
```

When the browser is unavailable, `browser_navigate` **silently degrades** to a
plain HTTP fetch. The `"method": "http"` field in the response is the signal —
only `"method": "browser"` proves the real browser was used. Check that field
first when debugging; it is faster than reading logs.

#### Other common traps

| Symptom | Cause / fix |
|---------|-------------|
| Screenshots full of tofu boxes | Image is missing CJK fonts, so `browser_vision` literally cannot see the text. Install `font-noto-cjk` (Alpine) / `fonts-noto-cjk` (Debian) |
| Browser starts then exits immediately | Root filesystem too small: Chrome needs a writable `/tmp` and profile dir. `--disable-dev-shm-usage` is already on by default; mount a tmpfs on `/tmp` if needed |
| `chrome_crashpad` errors spamming the log | Normal container noise, harmless |
| Memory exhausted / OOM | A resident headless Chrome costs 200–500MB. Cap concurrent tabs, or raise the container memory limit |
| Pages load but the login state keeps vanishing | `browser_profile_dir` was explicitly set to `""` (fresh temp profile every time), or the profile volume is not mounted / has the wrong owner (see the ownership note in compose) |

---

## 15. MCP & ACP

### 15.1 MCP (Model Context Protocol)

Connect external MCP servers and mount their tools into the agent (tool names prefixed `mcp_*`). Supports stdio and SSE transports.

```bash
magic mcp add <server-name>
magic mcp connect <server-name> <command> [args...]     # stdio
magic mcp list            # list connected servers and their tools
magic mcp health [server-name]
magic mcp disconnect <server-name>
```

Config goes under `mcp.servers` in `config.json`:

```jsonc
"mcp": {
  "servers": {
    "filesystem": {
      "command": "npx",
      "args": ["-y", "@modelcontextprotocol/server-filesystem", "/data"],
      "transport": "stdio"
    }
  }
}
```

### 15.2 ACP (Agent Communication Protocol)

Lets multiple agents discover each other and call each other's skills.

```bash
magic acp serve --transport stdio                    # default stdio
magic acp serve --transport http --address ":8080" --agent-id my-agent

magic acp connect helper --transport http --address http://localhost:8080
magic acp connect local-helper --transport stdio --command /path/to/agent --args "--mode","acp"
magic acp list
magic acp ping [agent]
magic acp skills [agent]
magic acp call helper summarize --params '{"text":"..."}'
magic acp disconnect helper
```

---

## 16. Scheduled Tasks (Cron)

```bash
# Have the agent run a prompt at a scheduled time
magic cron add morning-brief \
  --schedule "0 9 * * *" \
  --prompt "summarize yesterday's unfinished tasks" \
  --enabled

# Or skip the agent and run a script directly
magic cron add nightly-backup \
  --schedule "0 2 * * *" \
  --no-agent \
  --script "/usr/local/bin/backup.sh"

magic cron list
magic cron edit <name>
magic cron toggle <name>
magic cron test <name>       # run once immediately
magic cron remove <name> [--force]
```

`--schedule` uses the standard 5-field cron expression, default `0 9 * * *`.

---

## 17. Kanban Board

A task board for multi-agent collaboration. Status flow:

```
triage/todo → ready → running → done → archived
                   ↘ blocked → ready
```

```bash
magic kanban create "build login page" --body "support phone and email" \
    --priority 2 --assignee coder --tenant default -w "dir:/data/proj"
magic kanban list --status running --assignee coder --tenant default
magic kanban show <task_id>
magic kanban start <task_id>          # triage/todo → ready
magic kanban claim <task_id>          # ready → running
magic kanban complete <task_id> -s "shipped"   # running → done
magic kanban block <task_id> -r "waiting for design"
magic kanban unblock <task_id>
magic kanban comment <task_id> "note: must support dark mode"
magic kanban link <parent_id> <child_id>     # parent-child dependency
magic kanban unlink <parent_id> <child_id>
magic kanban triage <task_id>                # refine task with LLM
magic kanban board
magic kanban stats
magic kanban archive <task_id>
magic kanban delete <task_id>
```

`--priority`: `0=low`, `1=medium`, `2=high`, `3=critical`.
`--workspace`: supports `scratch`, `dir:/path`, `worktree` forms.

> ℹ️ `magic kanban create`'s `-p` shorthand has been removed (use `--priority` instead); see [22. Known Issues](#22-known-issues-verified).

---

## 18. Token Usage & Budget

```bash
magic usage            # today (default 1 day)
magic usage -d 7       # last 7 days
magic usage -d 30      # last 30 days
```

Inside the TUI use `/usage` and `/insights`. The web `/usage` page provides: today's stats, daily trend chart, monthly breakdown, budget progress bar & alerts, and top models/sessions.

Related APIs: `/api/usage/today|daily|weekly|monthly|insights|budget|sessions|top-sessions|providers|hourly`.

---

## 19. Privacy Redaction

Auto-detects and masks: phone numbers, emails, ID cards, bank cards, IPs, addresses; supports custom regexes.

```bash
magic privacy detect "contact me at 13800138000 or a@b.com"     # detect only, no masking
magic privacy detect "..." --json
magic privacy redact "contact me at 13800138000"
magic privacy redact --disable        # turn off redaction
magic privacy redact --enable         # turn on redaction
magic privacy audit                   # redaction audit log
magic privacy stats                   # stats
```

Config: `privacy.enabled`, the per-`redact_*` switches, and `custom_patterns`.

---

## 20. Operations: Diagnostics, Backup, Update

### 20.1 Diagnostics

```bash
magic doctor                       # run all checks by default
magic doctor -c config             # config only
magic doctor -c provider           # provider connectivity only
magic doctor -c tools              # tool availability only
magic doctor -c gateway            # gateway only
magic doctor -c skills             # skill dirs only
magic doctor all

magic status                       # config / skills / plugins / tools / resources / health
magic health
magic metrics
magic stats                        # config summary + skill count + session DB + log count
magic validate                     # validate config file
```

### 20.2 Logs

```bash
magic logs list
magic logs show magic_2026-09-01_14-42-28.log
magic logs latest
```

### 20.3 Backup

```bash
magic backup
```

Copies the entire magic home to `~/.magic_backup_YYYYMMDD_HHMMSS`, including config.json, skills/, sessions.db, logs/, SOUL.md, MEMORY.md, USER.md.

### 20.4 Update

```bash
magic update --check                      # check only
magic update                              # update (backs up first by default)
magic update --channel beta               # stable | beta | nightly
magic update --force                      # reinstall even if same version
magic update --backup=false               # no backup
```

### 20.5 Migration

```bash
magic migrate        # migrate config and data from OpenClaw to go-magic
```

### 20.6 Shell completion

```bash
magic completion bash
magic completion zsh
magic completion fish
magic completion powershell
```

### 20.7 Generated docs

```bash
magic docs generate --format markdown
magic docs serve --port 8080
magic docs man
```

---

## 21. FAQ

**Q: Do I need to restart after changing config?**
Provider / model switching is hot-reloaded and takes effect immediately in the Web Dashboard. For other items (e.g. `bot_mode.enabled`, gateway platforms), restart the relevant service.

**Q: How do I configure multiple models under one provider?**
List several in `providers.<name>.models`; **the first is the active model**. Switch in the TUI with `/model provider:model`, or in the CLI with `magic model provider:model`.

**Q: How do I connect an OpenAI-compatible service not in the list?**
Use the `custom` provider, or set a `base_url` on any provider.

**Q: How do bots talk to each other?**
Bots have a built-in `message_agent` tool; in the CLI it's `magic bots message <from> <to> <msg>` (fire-and-forget; the reply reaches the sender as a background notification). Requires `bot_mode.enabled = true`.

**Q: Dashboard throws a CORS error?**
By default only localhost / 127.0.0.1 on ports 8642 and 8643 are allowed. Append allowed origins with `GO_MAGIC_CORS_ORIGINS`, comma-separated.

**Q: I want bots on two go-magic instances to talk to each other?**
Use `magic peer`, see [10.6 Cross-machine Peer](#106-cross-machine-peer). Remember to set `bot_mode.relay_token` on the receiver.

**Q: Can't reach the page after Docker starts?**
Caused by the port mismatch; see the next section.

---

## 22. Known Issues (verified)

The following were reproduced on the **v0.5.9 compiled binary**. Items marked "✅ Fixed" were fixed in this session.

### 22.1 Two sub-commands panic on a flag conflict ✅ Fixed

The global persistent flag `-p` (`--profile`) conflicts with the local `-p` flag of two sub-commands, causing a panic on execution:

```bash
$ magic coding test
panic: unable to redefine 'p' shorthand in "test" flagset: it's already used for "pattern" flag

$ magic kanban create
panic: unable to redefine 'p' shorthand in "create" flagset: it's already used for "priority" flag
```

- `coding.go`'s `test` defines `-p/--pattern`
- `kanban.go`'s `create` defines `-p/--priority`

**Fix**: the `-p` shorthand was removed from both sub-commands (`coding test` now uses `--pattern`, `kanban create` now uses `--priority`), keeping the global `-p/--profile`. They are now usable directly.

### 22.2 Docker port mismatch ✅ Fixed

- `magic server`'s default port is **5000**
- `Dockerfile` / `docker-compose.yml` / `Makefile` expose **8642 (API) and 8643 (reserved, nothing listens)**
- The container's default command is `magic server`, so starting compose as-is listens on 5000 inside the container, which doesn't match the `8642:8642` mapping

**Fix**: the `Dockerfile` `CMD` now reads `["server", "--port", "8642"]`, `docker-compose.yml` port mapping is `8642:8642`, and `Makefile docker-run` dropped the invalid `GO_MAGIC_PROFILE` and fixed the port. The `:8642` example in `peers.go`'s help text was also corrected.

### 22.3 Docs didn't match actual env vars ✅ Fixed

README and the auto-generated docs used to mention `GO_MAGIC_PROFILE`, `MAGIC_HOME`, `MAGIC_PROFILE`, `MAGIC_VERBOSE`, `MAGIC_NO_COLOR`, none of which are **actually read by the code** (they only appeared in the doc text of `cmd/magic/docs.go`, `internal/docs/llm_generator.go`).

**Fix**: `README.md`, `README.zh-CN.md`, `cmd/magic/docs.go`, and `internal/docs/llm_generator.go` now all use the real variables `GO_MAGIC_HOME`, `GO_MAGIC_CORS_ORIGINS`, `MAGIC_SKILL_DIR`, `MAGIC_SESSION_ID`. Profile switching still uses the `--profile/-p` flag or the `profile` field in the config file.

### 22.4 Gateway reserved ports taken by platform callbacks + all webhooks unreachable in containers ✅ Fixed

Three intertwined problems:

1. **The DingTalk callback port collided with the gateway's embedded API.** `internal/gateway/dingtalk.go` set the callback port to `8080`, while the gateway's own embedded API is pinned to `127.0.0.1:8080`. Inside one process `0.0.0.0:8080` and `127.0.0.1:8080` cannot coexist, so the callback server's `ListenAndServe` failed with `EADDRINUSE` and DingTalk never received a single message — the error only ever reached the log.
2. **The Feishu callback port collided with the health-check server.** Same story: `feishu.go` used `8081`, while the health server binds `127.0.0.1:8081` (the `http://localhost:8081/health` endpoint documented in this file). The health server starts first, so the Feishu callback could never come up.
3. **Every webhook-based platform was unreachable under Docker.** The `Dockerfile` only did `EXPOSE 8642 8643` and `docker-compose.yml` only published those two, while each platform's callback listens on its own port — none of them were ever mapped out of the container.

**Fix**:

- DingTalk moved to **8091** and Feishu to **8092**, clear of the gateway's reserved 8080/8081.
- 8080/8081 are now single-sourced as `gateway.DefaultAPIPort` / `gateway.DefaultHealthPort`; `internal/server` and `cmd/magic/health.go` reference them instead of repeating the literals (that duplication is exactly how the collisions went unnoticed).
- `EXPOSE` in the `Dockerfile` and `ports` in `docker-compose.yml` now list the real callback ports: **8091 DingTalk, 8092 Feishu, 8084 Discord, 8085 Slack, 8087 LINE, 8088 Teams, 8089 Google Chat, 8090 SMS**. Only publish the ones you actually use; delete the rest.
- Related correction: **nothing listens on `8643`** (it only appears in the CORS allow-list), so the old claim that platform callbacks "go through 8643" was wrong.

> **Upgrade note**: after switching to 8091/8092, update the port in the webhook URLs registered in the DingTalk/Feishu consoles as well.

### 22.5 Assorted Docker backend and sandbox fixes ✅ Fixed

- **`DockerBackend`'s working directory could never work**: it passed `-w <host path>` without any `-v` mount, so the path did not exist inside the container and `docker run` failed with `no such file or directory`. The host directory is now bind-mounted at the *same path* and used as the working directory, matching the local backend's path semantics. Also added `--network` (the `networkMode` field used to be dead config), `--privileged` (still off by default, only honoured when explicitly enabled), and `-e HOME=/tmp` (without it a non-root uid leaves go/npm unable to create their caches).
- **Backend probes could block forever**: `IsAvailable()` / `Health()` probed `docker`/`ssh`/`daytona`/`singularity`/`modal` with a bare `exec.Command`, so a hung binary (unreachable daemon, ssh waiting on a host-key prompt) would hang the backend listing and `/health` along with it. They all go through `probeBinary` with a 5s timeout now.
- **`DockerBackend.Execute` ignored its `timeout` argument**: callers never put a deadline on `ctx`, so that argument is the only timeout mechanism — the old code ignored it entirely and a command could hang indefinitely. It now derives a deadline from it when `ctx` has none.
- **Sandbox path escape and command injection** (`internal/sandbox`, which currently has no callers):
  - `DockerSandbox`'s file operations fed host absolute paths to container commands (the container only mounts `/workspace`, so `cat`/`rm` always failed). Paths are now mapped to `/workspace/...` and `..` traversal is rejected.
  - `WriteFile` interpolated the payload into a shell heredoc (`cat > f << 'EOF'`), which broke on any data containing a line equal to `EOF` and was an injection surface; it now pipes over stdin (`-i`) with the destination passed as a positional argument rather than parsed as script text. The useless temp-file write is gone.
  - `BasicSandbox.isPathSafe` compared absolute paths with `strings.HasPrefix`, which has the classic **prefix flaw** (`/tmp/abc-evil` passed as a child of `/tmp/abc`) and never joined the argument with `workDir`, so every relative path was rejected. Containment is now decided with `filepath.Rel` after resolving symlinks.
  - Added `internal/sandbox/sandbox_test.go` covering the path-containment logic (previously zero coverage).

---

## Appendix: One-Minute Cheat Sheet

```bash
# First time
magic setup                       # wizard
magic doctor                      # self-check

# Daily
magic chat                        # TUI chat
magic server                      # Web Dashboard :5000
magic model deepseek:deepseek-chat
magic usage -d 7

# Bot
magic config set bot_mode.enabled true
magic bots create researcher --title "Research Assistant" --prompt "find and summarize papers"
magic bots chat researcher "find the latest papers on agent memory"
magic bots routine add researcher daily-digest --schedule "0 9 * * *" --prompt "summarize yesterday"

# Group chat
magic rooms create review --members researcher,coder --topic "review"

# Cross-machine
magic peer add lab-b http://192.168.1.20:5000 --token secret
magic peer dm lab-b researcher "how is it going?"

# Gateway
magic gateway setup
magic gateway start -P telegram

# Operations
magic backup
magic logs latest
magic update --check
```

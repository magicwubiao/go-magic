# Glossary

Terminology reference for go-magic project.

## Table of Contents

- [Core Concepts](#core-concepts)
- [Cortex Architecture](#cortex-architecture)
- [Three-Layer Architecture](#three-layer-architecture)
- [Six Self-Evolution Systems](#six-self-evolution-systems)
- [Plugin, Tool, Skill](#plugin-tool-skill)
- [CLI Commands](#cli-commands)

## Core Concepts

### Agent

An autonomous entity that can perceive, reason, and act. In go-magic, the Agent coordinates all systems to process user requests and generate responses.

### LLM (Large Language Model)

A neural network trained on vast amounts of text data to generate human-like responses. go-magic supports multiple LLM providers.

### Context Window

The maximum amount of text (measured in tokens) that an LLM can process in a single request. go-magic manages context to prevent overflow through compression.

### Token

The basic unit of text processing for LLMs. Typically 1 token ≈ 4 characters in English.

## Cortex Architecture

### Cortex

The core cognitive architecture of go-magic, implementing a three-layer system with six self-evolution capabilities.

### Perception

The first layer of Cortex, responsible for understanding user input.

### Cognition

The second layer of Cortex, responsible for planning and decision-making.

### Execution

The third layer of Cortex, responsible for task completion and validation.

### Nudge

An asynchronous signal triggered by the Message Trigger system to initiate background review without blocking the user.

### Frozen Snapshot

A caching mechanism that preserves the conversation prefix to reduce API costs by up to 90%.

### DAG (Directed Acyclic Graph)

A graph structure used in the Cognition layer to represent task dependencies and execution order.

## Three-Layer Architecture

### Perception Layer

| Term | Definition |
|------|------------|
| Intent Classification | Determining the user's intent (task, question, chat, etc.) |
| Complexity Assessment | Evaluating task difficulty (simple, moderate, advanced) |
| Entity Extraction | Identifying key entities in user input |
| Noise Detection | Filtering out non-essential content |

### Cognition Layer

| Term | Definition |
|------|------------|
| Task Planning | Breaking down complex tasks into subtasks |
| DAG Management | Organizing subtasks based on dependencies |
| Adaptive Max Turns | Dynamically adjusting the maximum number of conversation turns |
| Sub-agent Decision | Determining when to spawn parallel sub-agents |

### Execution Layer

| Term | Definition |
|------|------------|
| Checkpoint | Saving progress at specific points in task execution |
| Resume Support | Continuing from a saved checkpoint after interruption |
| Result Validation | Verifying task outputs meet requirements |
| Progress Tracking | Monitoring task completion status |

## Six Self-Evolution Systems

### Message Trigger

Detects conversation turns (human → AI → human) and triggers Nudge signals for background processing.

### Nudge System

An asynchronous processing system that performs background review without blocking user interaction.

### Background Review

Analyzes conversation patterns and generates skill drafts based on detected usage patterns.

### Frozen Snapshot

Protects the conversation prefix cache, dramatically reducing API costs for multi-turn conversations.

### FTS (Full-Text Search) Memory

Enables searching across all historical sessions using BM25 ranking algorithm.

### Skill Auto-Evolution

Progressive disclosure system where skills evolve based on usage:
- **Level 0**: Minimal information, lowest cost
- **Level 1**: Basic information
- **Level 2**: Full information

## Plugin, Tool, Skill

### Tool

| Aspect | Description |
|--------|-------------|
| Purpose | Atomic executable functions |
| Loading | Built-in/static |
| Examples | `read_file`, `write_file`, `web_search` |
| Scope | Low-level operations |

### Plugin

| Aspect | Description |
|--------|-------------|
| Purpose | Dynamic extension modules |
| Loading | Runtime/dynamic loading |
| Examples | Custom LLM adapters, protocol implementations |
| Scope | System-level extensions |

### Skill

| Aspect | Description |
|--------|-------------|
| Purpose | High-level task templates |
| Loading | Lazy/progressive |
| Examples | Code generation, debugging, documentation |
| Scope | Task-level assistance |

### Comparison

| Feature | Tool | Plugin | Skill |
|---------|------|--------|-------|
| Granularity | Atomic | Module | Template |
| Loading | Static | Dynamic | Lazy |
| Cost Impact | Per-use | System | Progressive |
| User Control | Full | Admin | Auto-evolve |
| Examples | 15+ built-in | MCP, gateway | coding-assistant |

## CLI Commands

| Command | Category | Description |
|---------|----------|-------------|
| `chat` | Core | Interactive chat session |
| `agent` | Core | Run agent mode with parallel execution |
| `repl` | Core | Read-Eval-Print Loop |
| `voice` | Core | Voice interaction mode |
| `config` | Configuration | Configuration management |
| `skills` | Skills | Skills management |
| `plugin` | Plugin | Plugin management |
| `session` | Session | Session management |
| `tools` | Tools | Tool utilities |
| `version` | Utilities | Show version information |
| `health` | Utilities | Health check |
| `doctor` | Utilities | Run diagnostics |

## Message Types

| Type | Description |
|------|-------------|
| `task` | User requesting a specific task |
| `question` | User asking a question |
| `chat` | Casual conversation |
| `system` | System-level request |
| `error` | Error reporting |
| `feedback` | User feedback |

## Complexity Levels

| Level | Description | Typical Max Turns |
|-------|-------------|-------------------|
| `simple` | Single operation, no planning needed | 5 |
| `moderate` | Multiple operations, basic planning | 15 |
| `advanced` | Complex multi-step tasks, detailed planning | 25+ |

## Security Terms

| Term | Definition |
|------|------------|
| Command Whitelist | List of allowed shell commands |
| Injection Detection | Identifying malicious input patterns |
| Dangerous Pattern | Regex patterns for blocking harmful operations |
| PII Redaction | Automatic detection and removal of personally identifiable information |

## Storage Terms

| Term | Definition |
|------|------------|
| SQLite | Local database for session storage |
| FTS Store | Full-text search index for memory |
| Frozen Snapshot | Cached conversation prefix |
| Session | Persistent chat conversation |

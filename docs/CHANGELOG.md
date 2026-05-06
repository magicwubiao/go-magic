# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
- Complete documentation system regeneration
- New documentation structure with unified style

### Changed
- Refactored Hermes to Cortex naming throughout the codebase
- Updated all documentation references to use new package paths

### Removed
- Removed legacy implementation documents (P0-P2)
- Removed archive directory with old phase completion reports
- Removed CORTEX_MIGRATION.md (no longer needed)
- Removed ARCHITECTURE_PLUGIN_TOOL_SKILL.md (consolidated into architecture docs)

---

## [v1.0.0] - 2024-05-05

### Added

#### Cortex Architecture
- Three-layer cognitive architecture (Perception → Cognition → Execution)
- Six self-evolution systems:
  - Message Trigger
  - Nudge System
  - Background Review
  - Frozen Snapshot (90% API cost savings)
  - FTS Memory
  - Skill Auto-Evolution

#### Core Features
- Multi-provider support (OpenAI, DeepSeek, Huoshan, Anthropic, Zhipu, Kimi, MiniMax, etc.)
- Comprehensive plugin system with dynamic loading
- Extended tool system with 15+ built-in tools
- Skills framework with auto-creation capabilities
- MCP protocol integration
- Subagent parallel execution
- PII redaction

#### CLI Commands
- `magic chat` - Interactive chat session
- `magic agent` - Agent mode with parallel execution
- `magic repl` - Read-Eval-Print Loop
- `magic voice` - Voice interaction mode
- `magic config` - Configuration management
- `magic skills` - Skills management
- `magic plugin` - Plugin management
- `magic session` - Session management
- `magic tools` - Tool utilities
- Shell completion support (bash, zsh, fish)

#### Security Features
- Command whitelist
- Injection detection
- Dangerous pattern blocking
- PII detection and redaction

### Changed

#### Architecture Refactoring
- Replaced Hermes with Cortex as primary naming
- Complete internal package restructure under `internal/cortex/`
- Progressive renaming with backward compatibility removed

#### Documentation
- Complete architecture overview
- API documentation
- Tool development guide
- Plugin development guide
- Configuration guide

---

## [v0.9.0] - 2024-05-01

### Added
- Comprehensive CLI enhancement
- Tool system enhancement
- Hermes-Agent integration improvements

### Fixed
- String conversion bug
- Duplicate init() in main.go and skill_list.go

---

## [v0.8.0] - 2024-04-20

### Added
- Plugin system implementation
- Architecture analysis for Plugin, Tool, Skill relationship

---

## [v0.7.0] - 2024-04-15

### Added
- Continuous Hermes optimization
- Enhanced all six systems

---

## [v0.6.0] - 2024-04-10

### Added
- Cognition Layer implementation (Phase 3)
- Task planning with DAG management
- Adaptive max turns
- Sub-agent decisions

---

## [v0.5.0] - 2024-04-05

### Added
- Perception Layer implementation (Phase 2)
- Intent classification (7 types)
- Complexity assessment (3 levels)
- Entity extraction
- Noise detection

---

## [v0.4.0] - 2024-04-01

### Added
- Hermes Agent six-system optimization (Phase 1)
- Message Trigger
- Nudge System
- Background Review
- Frozen Snapshot
- FTS Memory
- Skill Auto-Evolution

---

## [v0.3.0] - 2024-03-25

### Added
- Unit tests and benchmarks
- Hermes-Agent integration improvements

---

## [v0.2.0] - 2024-03-20

### Added
- Complete Hermes Agent full architecture (Phase 4 FINAL)
- Integration tests

---

## [v0.1.0] - 2024-03-15

### Added
- Initial project setup
- Core agent functionality
- Basic tool system
- Gateway support

---

## Migration Notes

### v1.0.0 (Hermes → Cortex)

If you were using Hermes:

1. Update imports:
   ```go
   // Old
   import "github.com/magicwubiao/go-magic/internal/hermes"
   
   // New
   import "github.com/magicwubiao/go-magic/internal/cortex"
   ```

2. Update configuration:
   ```json
   {
     "hermes": { ... }  // Remove
     "cortex": { ... }  // Add
   }
   ```

3. Update CLI commands:
   ```bash
   magic hermes ...  # Removed
   magic cortex ...  # Use instead
   ```

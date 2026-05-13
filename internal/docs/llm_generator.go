package docs

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// LLMConfig holds configuration for LLM documentation
type LLMConfig struct {
	OutputDir  string   // Output directory
	IncludeAll bool     // Include all files, not just curated
	Title      string   // Documentation title
	BaseURL    string   // Base URL for links
	Version    string   // Version string
}

// LLMGenerator generates machine-readable documentation
type LLMGenerator struct {
	config *LLMConfig
}

// NewLLMGenerator creates a new LLM documentation generator
func NewLLMGenerator(config *LLMConfig) *LLMGenerator {
	if config == nil {
		config = &LLMConfig{
			Title:     "go-magic Documentation",
			OutputDir: "docs",
		}
	}
	return &LLMGenerator{config: config}
}

// DocumentationEntry represents a documentation entry
type DocumentationEntry struct {
	Title       string
	Description string
	Path        string
	Category    string
	Tags        []string
}

// GenerateLLMsTxt generates the llms.txt file (curated index)
func (g *LLMGenerator) GenerateLLMsTxt() (string, error) {
	var buf bytes.Buffer
	
	// Header
	buf.WriteString("# go-magic Documentation\n\n")
	buf.WriteString("> Machine-readable documentation index for LLM context loading.\n\n")
	buf.WriteString("Generated: " + time.Now().Format("2006-01-02") + "\n")
	buf.WriteString("Version: " + g.config.Version + "\n\n")
	buf.WriteString("---\n\n")
	
	// Curated sections
	sections := []struct {
		name  string
		items []DocumentationEntry
	}{
		{"Getting Started", g.getGettingStartedEntries()},
		{"CLI Commands", g.getCLICommandEntries()},
		{"Configuration", g.getConfigurationEntries()},
		{"Tools & Toolsets", g.getToolsEntries()},
		{"Skills System", g.getSkillsEntries()},
		{"Messaging Gateway", g.getGatewayEntries()},
		{"Features", g.getFeatureEntries()},
	}
	
	for _, section := range sections {
		if len(section.items) == 0 {
			continue
		}
		buf.WriteString(fmt.Sprintf("## %s\n\n", section.name))
		for _, item := range section.items {
			buf.WriteString(fmt.Sprintf("### %s\n", item.Title))
			buf.WriteString(fmt.Sprintf("%s\n", item.Description))
			if item.Path != "" {
				buf.WriteString(fmt.Sprintf("Path: %s\n", item.Path))
			}
			buf.WriteString("\n")
		}
	}
	
	return buf.String(), nil
}

// GenerateLLMsFullTxt generates the full documentation (llms-full.txt)
func (g *LLMGenerator) GenerateLLMsFullTxt() (string, error) {
	var buf bytes.Buffer
	
	// Header
	buf.WriteString("# go-magic Complete Documentation\n\n")
	buf.WriteString("> Complete documentation concatenated for one-shot LLM ingestion.\n\n")
	buf.WriteString("Generated: " + time.Now().Format("2006-01-02") + "\n")
	buf.WriteString("Version: " + g.config.Version + "\n\n")
	buf.WriteString("---\n\n")
	
	// Generate all sections
	buf.WriteString(g.generateGettingStarted())
	buf.WriteString(g.generateCLIReference())
	buf.WriteString(g.generateConfiguration())
	buf.WriteString(g.generateToolsReference())
	buf.WriteString(g.generateSkillsReference())
	buf.WriteString(g.generateGatewayReference())
	buf.WriteString(g.generateFeaturesReference())
	
	return buf.String(), nil
}

// WriteFiles writes the documentation files
func (g *LLMGenerator) WriteFiles() error {
	// Create output directory
	if err := os.MkdirAll(g.config.OutputDir, 0755); err != nil {
		return err
	}
	
	// Write llms.txt
	llms, err := g.GenerateLLMsTxt()
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(g.config.OutputDir, "llms.txt"), []byte(llms), 0644); err != nil {
		return err
	}
	
	// Write llms-full.txt
	llmsFull, err := g.GenerateLLMsFullTxt()
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(g.config.OutputDir, "llms-full.txt"), []byte(llmsFull), 0644)
}

// getGettingStartedEntries returns curated getting started entries
func (g *LLMGenerator) getGettingStartedEntries() []DocumentationEntry {
	return []DocumentationEntry{
		{
			Title:       "Installation",
			Description: "Install go-magic using curl installer or from source. Requires Go 1.21+.",
			Path:        "/getting-started/installation",
		},
		{
			Title:       "Quickstart",
			Description: "Get started with go-magic in 2 minutes. Configure provider and start chatting.",
			Path:        "/getting-started/quickstart",
		},
		{
			Title:       "Configuration",
			Description: "Configure AI providers, tools, gateway, and all options.",
			Path:        "/user-guide/configuration",
		},
	}
}

// getCLICommandEntries returns CLI command entries
func (g *LLMGenerator) getCLICommandEntries() []DocumentationEntry {
	return []DocumentationEntry{
		{
			Title:       "magic setup",
			Description: "Interactive setup wizard. Configures provider, model, tools, and gateway.",
			Tags:        []string{"cli", "setup"},
		},
		{
			Title:       "magic doctor",
			Description: "Diagnose configuration and connection issues.",
			Tags:        []string{"cli", "diagnostics"},
		},
		{
			Title:       "magic model",
			Description: "Select and configure AI provider and model.",
			Tags:        []string{"cli", "model"},
		},
		{
			Title:       "magic tools",
			Description: "Manage tools and toolsets. List, enable, disable tools.",
			Tags:        []string{"cli", "tools"},
		},
		{
			Title:       "magic skills",
			Description: "Manage skills. List, search, install from Hub.",
			Tags:        []string{"cli", "skills"},
		},
		{
			Title:       "magic gateway",
			Description: "Configure and manage messaging gateway (Telegram, Discord, etc.).",
			Tags:        []string{"cli", "gateway"},
		},
		{
			Title:       "magic sessions",
			Description: "Manage conversation sessions. List, switch, delete sessions.",
			Tags:        []string{"cli", "sessions"},
		},
		{
			Title:       "magic update",
			Description: "Check for and install updates. Supports auto-backup and rollback.",
			Tags:        []string{"cli", "update"},
		},
		{
			Title:       "magic migrate",
			Description: "Migrate from OpenClaw configuration and data.",
			Tags:        []string{"cli", "migration"},
		},
		{
			Title:       "magic secrets",
			Description: "Manage encrypted API keys and secrets.",
			Tags:        []string{"cli", "secrets"},
		},
		{
			Title:       "magic prompt",
			Description: "Manage prompt templates for different use cases.",
			Tags:        []string{"cli", "prompt"},
		},
		{
			Title:       "magic usage",
			Description: "View token usage statistics and cost tracking.",
			Tags:        []string{"cli", "usage"},
		},
		{
			Title:       "magic audit",
			Description: "View audit logs for sensitive operations.",
			Tags:        []string{"cli", "audit"},
		},
		{
			Title:       "magic personality",
			Description: "Manage agent personalities and behavior modes.",
			Tags:        []string{"cli", "personality"},
		},
		{
			Title:       "magic context",
			Description: "Manage project context files that shape agent behavior.",
			Tags:        []string{"cli", "context"},
		},
		{
			Title:       "magic compress",
			Description: "Compress conversation context to save tokens.",
			Tags:        []string{"cli", "compression"},
		},
	}
}

// getConfigurationEntries returns configuration entries
func (g *LLMGenerator) getConfigurationEntries() []DocumentationEntry {
	return []DocumentationEntry{
		{
			Title:       "Provider Configuration",
			Description: "Configure AI providers: OpenAI, Anthropic, DeepSeek, Ollama, OpenRouter.",
			Path:        "/user-guide/configuration#providers",
		},
		{
			Title:       "Tools Configuration",
			Description: "Enable/disable toolsets. Configure individual tool options.",
			Path:        "/user-guide/configuration#tools",
		},
		{
			Title:       "Gateway Configuration",
			Description: "Configure messaging platforms: Telegram, Discord, Slack, WhatsApp.",
			Path:        "/user-guide/configuration#gateway",
		},
		{
			Title:       "Environment Variables",
			Description: "All supported environment variables for configuration.",
			Path:        "/reference/environment-variables",
		},
	}
}

// getToolsEntries returns tools entries
func (g *LLMGenerator) getToolsEntries() []DocumentationEntry {
	return []DocumentationEntry{
		{
			Title:       "Web Tools",
			Description: "web_search, web_extract - Search and extract web content.",
			Tags:        []string{"tools", "web"},
		},
		{
			Title:       "Browser Tools",
			Description: "browser_navigate, browser_snapshot, browser_click, browser_type, browser_scroll - Browser automation.",
			Tags:        []string{"tools", "browser"},
		},
		{
			Title:       "Terminal Tools",
			Description: "execute_command, terminal, process - Execute commands with multiple backends.",
			Tags:        []string{"tools", "terminal"},
		},
		{
			Title:       "File Tools",
			Description: "read_file, write_file, file_edit, list_files, search_in_files - File operations.",
			Tags:        []string{"tools", "file"},
		},
		{
			Title:       "Code Execution",
			Description: "execute_code - Run Python code with tool access.",
			Tags:        []string{"tools", "code"},
		},
		{
			Title:       "Skills Tools",
			Description: "skill_list, skill_view, skill_manage, skill_create, skill_delete - Skill management.",
			Tags:        []string{"tools", "skills"},
		},
		{
			Title:       "Memory Tools",
			Description: "memory_store, memory_recall - Persistent memory storage.",
			Tags:        []string{"tools", "memory"},
		},
		{
			Title:       "Delegation Tools",
			Description: "delegate_task, poll_task, list_tasks, cancel_task - Sub-agent delegation.",
			Tags:        []string{"tools", "delegation"},
		},
		{
			Title:       "Home Assistant Tools",
			Description: "ha_list_entities, ha_get_state, ha_call_service - Smart home control.",
			Tags:        []string{"tools", "homeassistant"},
		},
		{
			Title:       "Utility Tools",
			Description: "json, yaml, string, hash, uuid, random, time, math, csv, env, system_info.",
			Tags:        []string{"tools", "utility"},
		},
	}
}

// getSkillsEntries returns skills entries
func (g *LLMGenerator) getSkillsEntries() []DocumentationEntry {
	return []DocumentationEntry{
		{
			Title:       "Skills System Overview",
			Description: "Skills are reusable, shareable prompts stored in markdown files.",
			Path:        "/user-guide/features/skills",
		},
		{
			Title:       "Creating Skills",
			Description: "Create custom skills with SKILL.md format. Support L0/L1/L2 progressive loading.",
			Tags:        []string{"skills", "creation"},
		},
		{
			Title:       "Skills Hub",
			Description: "Browse and install skills from agentskills.io community hub.",
			Tags:        []string{"skills", "hub"},
		},
		{
			Title:       "Conditional Activation",
			Description: "Skills can auto-activate based on available toolsets and platform.",
			Tags:        []string{"skills", "conditional"},
		},
	}
}

// getGatewayEntries returns gateway entries
func (g *LLMGenerator) getGatewayEntries() []DocumentationEntry {
	return []DocumentationEntry{
		{
			Title:       "Gateway Overview",
			Description: "Connect to Telegram, Discord, Slack, WhatsApp, and other platforms.",
			Path:        "/user-guide/messaging",
		},
		{
			Title:       "Telegram Setup",
			Description: "Configure Telegram bot for messaging access.",
			Tags:        []string{"gateway", "telegram"},
		},
		{
			Title:       "Discord Setup",
			Description: "Configure Discord bot for server access.",
			Tags:        []string{"gateway", "discord"},
		},
		{
			Title:       "Slack Setup",
			Description: "Configure Slack app for workspace access.",
			Tags:        []string{"gateway", "slack"},
		},
	}
}

// getFeatureEntries returns feature entries
func (g *LLMGenerator) getFeatureEntries() []DocumentationEntry {
	return []DocumentationEntry{
		{
			Title:       "MCP Integration",
			Description: "Connect to MCP servers for extended capabilities. Filter tools safely.",
			Path:        "/user-guide/features/mcp",
		},
		{
			Title:       "Memory System",
			Description: "Persistent memory with automatic summarization and recall.",
			Path:        "/user-guide/features/memory",
		},
		{
			Title:       "Terminal Backends",
			Description: "Execute commands locally, in Docker, SSH, Daytona, Singularity, or Modal.",
			Path:        "/user-guide/features/tools#terminal-backends",
		},
		{
			Title:       "Learning Loop",
			Description: "Agent auto-improves skills from experience and usage patterns.",
			Tags:        []string{"features", "learning"},
		},
		{
			Title:       "User Profiling",
			Description: "Cross-session user preference learning and adaptation.",
			Tags:        []string{"features", "profiling"},
		},
		{
			Title:       "Voice Mode",
			Description: "Real-time voice interaction with TTS and ASR support.",
			Tags:        []string{"features", "voice"},
		},
		{
			Title:       "Cron Scheduling",
			Description: "Schedule tasks to run at specific times with platform delivery.",
			Tags:        []string{"features", "cron"},
		},
		{
			Title:       "Context Compression",
			Description: "Summarize old messages to save context window tokens.",
			Tags:        []string{"features", "compression"},
		},
	}
}

// Section generators
func (g *LLMGenerator) generateGettingStarted() string {
	var buf bytes.Buffer
	buf.WriteString("# Getting Started\n\n")
	buf.WriteString("## Installation\n\n")
	buf.WriteString("```bash\n")
	buf.WriteString("# Quick install\n")
	buf.WriteString("curl -fsSL https://raw.githubusercontent.com/magicwubiao/go-magic/main/scripts/install.sh | bash\n\n")
	buf.WriteString("# From source\n")
	buf.WriteString("git clone https://github.com/magicwubiao/go-magic.git\n")
	buf.WriteString("cd go-magic\n")
	buf.WriteString("make build\n")
	buf.WriteString("```\n\n")
	buf.WriteString("## Quick Start\n\n")
	buf.WriteString("```bash\n")
	buf.WriteString("# Run setup wizard\n")
	buf.WriteString("magic setup\n\n")
	buf.WriteString("# Start chatting\n")
	buf.WriteString("magic\n\n")
	buf.WriteString("# Or with a specific command\n")
	buf.WriteString("magic chat \"Hello, help me with Go programming\"\n")
	buf.WriteString("```\n\n")
	return buf.String()
}

func (g *LLMGenerator) generateCLIReference() string {
	var buf bytes.Buffer
	buf.WriteString("# CLI Command Reference\n\n")
	
	commands := []struct {
		name        string
		description string
		subcommands string
	}{
		{"setup", "Interactive setup wizard", "  --skip-model, --skip-tools, --skip-gateway"},
		{"doctor", "Diagnose issues", "  --check config|provider|tools|gateway|skills"},
		{"model", "Model configuration", "list, set <provider:model>"},
		{"tools", "Tool management", "list [--json], toolsets list|show|enable|disable"},
		{"skills", "Skill management", "list, search <query>, show <name>, install <name>"},
		{"gateway", "Messaging gateway", "setup, start, stop, status"},
		{"sessions", "Session management", "list, switch <id>, delete <id>"},
		{"update", "Auto-update", "--check, --backup, --force, --channel beta|stable"},
		{"migrate", "Migration", "--dry-run, --preset user-data, --overwrite"},
		{"secrets", "Secrets management", "list, set <key>, get <key>, delete <key>, import-env"},
		{"prompt", "Prompt templates", "list, show <name>, create <name>, edit <name>, delete"},
		{"usage", "Usage statistics", "--daily, --monthly, --budget, --export, insights [--days N]"},
		{"audit", "Audit logs", "list [--user <name>], [--since <time>], export"},
		{"personality", "Agent personality", "list, set <name>, show <name>, create <name>"},
		{"context", "Project context", "load <path>, list, reload, clear"},
		{"compress", "Context compression", "--dry-run, --level default|full"},
		{"i18n", "Internationalization", "list, set <lang>, translate <key>"},
		{"learn", "Learning loop", "analyze, stats, improve <skill>"},
		{"profile", "User profile", "user, update <key>=<value>, clear"},
		{"mcp", "MCP management", "list, connect <name> <cmd>, disconnect <name>, health"},
		{"plugin", "Plugin system", "list, discover, load <path>, unload <name>"},
		{"voice", "Voice mode", "list-presets, set-preset <name>, test"},
	}
	
	for _, cmd := range commands {
		buf.WriteString(fmt.Sprintf("## magic %s\n\n", cmd.name))
		buf.WriteString(fmt.Sprintf("Description: %s\n\n", cmd.description))
		if cmd.subcommands != "" {
			buf.WriteString(fmt.Sprintf("Subcommands:\n%s\n\n", cmd.subcommands))
		}
	}
	
	return buf.String()
}

func (g *LLMGenerator) generateConfiguration() string {
	var buf bytes.Buffer
	buf.WriteString("# Configuration Reference\n\n")
	buf.WriteString("## Configuration File\n\n")
	buf.WriteString("Location: `~/.magic/config.yaml`\n\n")
	buf.WriteString("```yaml\n")
	buf.WriteString("provider:\n")
	buf.WriteString("  name: openai  # openai, anthropic, deepseek, ollama, openrouter\n")
	buf.WriteString("  model: gpt-4o\n")
	buf.WriteString("  api_key: ${OPENAI_API_KEY}\n\n")
	buf.WriteString("tools:\n")
	buf.WriteString("  enabled:\n")
	buf.WriteString("    - web\n")
	buf.WriteString("    - browser\n")
	buf.WriteString("    - terminal\n")
	buf.WriteString("    - file\n\n")
	buf.WriteString("gateway:\n")
	buf.WriteString("  enabled: true\n")
	buf.WriteString("  platforms:\n")
	buf.WriteString("    - telegram\n")
	buf.WriteString("```\n\n")
	buf.WriteString("## Environment Variables\n\n")
	buf.WriteString("| Variable | Description |\n")
	buf.WriteString("|----------|-------------|\n")
	buf.WriteString("| OPENAI_API_KEY | OpenAI API key |\n")
	buf.WriteString("| ANTHROPIC_API_KEY | Anthropic API key |\n")
	buf.WriteString("| DEEPSEEK_API_KEY | DeepSeek API key |\n")
	buf.WriteString("| MAGIC_HOME | Config directory (default: ~/.magic) |\n")
	buf.WriteString("| MAGIC_PROFILE | Active profile name |\n")
	buf.WriteString("```\n\n")
	return buf.String()
}

func (g *LLMGenerator) generateToolsReference() string {
	var buf bytes.Buffer
	buf.WriteString("# Tools Reference\n\n")
	buf.WriteString("## Toolset: web\n\n")
	buf.WriteString("- `web_search` - Search the web\n")
	buf.WriteString("- `web_extract` - Extract content from URLs\n\n")
	buf.WriteString("## Toolset: browser\n\n")
	buf.WriteString("- `browser_navigate` - Navigate to URL\n")
	buf.WriteString("- `browser_snapshot` - Get page snapshot\n")
	buf.WriteString("- `browser_click` - Click element\n")
	buf.WriteString("- `browser_type` - Type into input\n")
	buf.WriteString("- `browser_scroll` - Scroll page\n")
	buf.WriteString("- `browser_back` - Go back\n")
	buf.WriteString("- `browser_get_images` - Extract images\n")
	buf.WriteString("- `browser_console` - Get console messages\n\n")
	buf.WriteString("## Toolset: terminal\n\n")
	buf.WriteString("- `execute_command` - Execute shell command\n")
	buf.WriteString("- `terminal` - Terminal operations\n")
	buf.WriteString("- `process` - Process management\n\n")
	buf.WriteString("## Toolset: file\n\n")
	buf.WriteString("- `read_file` - Read file\n")
	buf.WriteString("- `write_file` - Write file\n")
	buf.WriteString("- `file_edit` - Edit file\n")
	buf.WriteString("- `list_files` - List directory\n")
	buf.WriteString("- `search_in_files` - Grep files\n\n")
	buf.WriteString("## Toolset: code_execution\n\n")
	buf.WriteString("- `execute_code` - Run Python code\n\n")
	buf.WriteString("## Toolset: skills\n\n")
	buf.WriteString("- `skill_list` - List skills\n")
	buf.WriteString("- `skill_view` - View skill\n")
	buf.WriteString("- `skill_manage` - Manage skills\n")
	buf.WriteString("- `skill_create` - Create skill\n")
	buf.WriteString("- `skill_delete` - Delete skill\n\n")
	return buf.String()
}

func (g *LLMGenerator) generateSkillsReference() string {
	var buf bytes.Buffer
	buf.WriteString("# Skills System\n\n")
	buf.WriteString("## SKILL.md Format\n\n")
	buf.WriteString("```markdown\n")
	buf.WriteString("---\n")
	buf.WriteString("name: my-skill\n")
	buf.WriteString("description: What this skill does\n")
	buf.WriteString("category: development\n")
	buf.WriteString("tags: [code, go]\n")
	buf.WriteString("---\n\n")
	buf.WriteString("# Level 0: Description\n")
	buf.WriteString("This skill helps with...\n\n")
	buf.WriteString("# Level 1: Full Content\n")
	buf.WriteString("Detailed instructions and prompts...\n\n")
	buf.WriteString("# Level 2: References\n")
	buf.WriteString("Additional reference materials...\n")
	buf.WriteString("```\n\n")
	buf.WriteString("## Progressive Loading\n\n")
	buf.WriteString("- L0: Only name and description (for listings)\n")
	buf.WriteString("- L1: Full content (for use)\n")
	buf.WriteString("- L2: With reference files (for complex skills)\n\n")
	buf.WriteString("## Conditional Activation\n\n")
	buf.WriteString("```yaml\n")
	buf.WriteString("fallback_for_toolset: web\n")
	buf.WriteString("requires_toolset: [browser]\n")
	buf.WriteString("platform: linux  # linux, macos, windows\n")
	buf.WriteString("```\n\n")
	return buf.String()
}

func (g *LLMGenerator) generateGatewayReference() string {
	var buf bytes.Buffer
	buf.WriteString("# Messaging Gateway\n\n")
	buf.WriteString("## Supported Platforms\n\n")
	buf.WriteString("- Telegram\n")
	buf.WriteString("- Discord\n")
	buf.WriteString("- Slack\n")
	buf.WriteString("- WhatsApp\n")
	buf.WriteString("- Signal\n")
	buf.WriteString("- Matrix\n")
	buf.WriteString("- Email (SMTP)\n")
	buf.WriteString("- SMS\n")
	buf.WriteString("- DingTalk\n")
	buf.WriteString("- Feishu\n")
	buf.WriteString("- WeCom\n")
	buf.WriteString("- Microsoft Teams\n\n")
	buf.WriteString("## Setup Example (Telegram)\n\n")
	buf.WriteString("```bash\n")
	buf.WriteString("magic gateway setup\n")
	buf.WriteString("# Select Telegram\n")
	buf.WriteString("# Enter bot token from @BotFather\n")
	buf.WriteString("magic gateway start\n")
	buf.WriteString("```\n\n")
	return buf.String()
}

func (g *LLMGenerator) generateFeaturesReference() string {
	var buf bytes.Buffer
	buf.WriteString("# Features\n\n")
	buf.WriteString("## MCP Integration\n\n")
	buf.WriteString("Connect to any MCP server:\n\n")
	buf.WriteString("```yaml\n")
	buf.WriteString("mcp:\n")
	buf.WriteString("  servers:\n")
	buf.WriteString("    github:\n")
	buf.WriteString("      command: npx\n")
	buf.WriteString("      args: [\"-y\", \"@modelcontextprotocol/server-github\"]\n")
	buf.WriteString("```\n\n")
	buf.WriteString("## Terminal Backends\n\n")
	buf.WriteString("| Backend | Description |\n")
	buf.WriteString("|---------|-------------|\n")
	buf.WriteString("| local | Direct execution |\n")
	buf.WriteString("| docker | Container isolation |\n")
	buf.WriteString("| ssh | Remote execution |\n")
	buf.WriteString("| daytona | Cloud sandbox |\n")
	buf.WriteString("| singularity | Container isolation |\n")
	buf.WriteString("| modal | Serverless |\n\n")
	buf.WriteString("## Learning Loop\n\n")
	buf.WriteString("go-magic automatically:\n")
	buf.WriteString("- Improves skills from usage\n")
	buf.WriteString("- Learns user preferences\n")
	buf.WriteString("- Summarizes conversations\n")
	buf.WriteString("- Creates new skills from complex tasks\n\n")
	buf.WriteString("## Profile System\n\n")
	buf.WriteString("Multiple isolated configurations:\n\n")
	buf.WriteString("```bash\n")
	buf.WriteString("magic profile create work\n")
	buf.WriteString("magic profile switch work\n")
	buf.WriteString("```\n\n")
	return buf.String()
}

// GenerateIndex generates a search-friendly index
func (g *LLMGenerator) GenerateIndex() (string, error) {
	var buf bytes.Buffer
	
	entries := []DocumentationEntry{}
	
	// Collect all entries
	for _, section := range [][]DocumentationEntry{
		g.getGettingStartedEntries(),
		g.getCLICommandEntries(),
		g.getConfigurationEntries(),
		g.getToolsEntries(),
		g.getSkillsEntries(),
		g.getGatewayEntries(),
		g.getFeatureEntries(),
	} {
		entries = append(entries, section...)
	}
	
	// Sort by title
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Title < entries[j].Title
	})
	
	buf.WriteString("# go-magic Index\n\n")
	buf.WriteString(fmt.Sprintf("Total entries: %d\n\n", len(entries)))
	
	for _, entry := range entries {
		buf.WriteString(fmt.Sprintf("- [%s](%s) - %s\n", entry.Title, entry.Path, entry.Description))
	}
	
	return buf.String(), nil
}

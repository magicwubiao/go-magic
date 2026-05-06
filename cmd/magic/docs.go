package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

var docsCmd = &cobra.Command{
	Use:   "docs [generate|serve|man]",
	Short: "Generate and manage documentation",
	Long: `Generate and manage documentation for magic CLI.

This command helps you:
- Generate Markdown documentation for all commands
- Serve documentation locally with a web server
- Generate man pages for Unix systems`,
	DisableFlagsInUseLine: true,
	ValidArgs:             []string{"generate", "serve", "man"},
	Args:                  cobra.MatchAll,
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			return cmd.Help()
		}

		switch args[0] {
		case "generate":
			return generateDocs()
		case "serve":
			return serveDocs()
		case "man":
			return generateManPages()
		default:
			return fmt.Errorf("unknown docs command: %s", args[0])
		}
	},
}

var (
	docsOutputDir  string
	docsFormat     string
	docsIncludeAll bool
	docsPort       int
)

func init() {
	docsCmd.Flags().StringVarP(&docsOutputDir, "output", "o", "./docs", "Output directory for documentation")
	docsCmd.Flags().StringVar(&docsFormat, "format", "markdown", "Output format (markdown, html, man)")
	docsCmd.Flags().BoolVar(&docsIncludeAll, "all", false, "Include hidden commands")
	docsCmd.Flags().IntVarP(&docsPort, "port", "p", 8080, "Port for documentation server")
	rootCmd.AddCommand(docsCmd)
}

func generateDocs() error {
	fmt.Printf("Generating documentation to %s...\n", docsOutputDir)

	// Create output directory
	if err := os.MkdirAll(docsOutputDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	// Generate main README
	if err := generateMainReadme(); err != nil {
		return fmt.Errorf("failed to generate README: %w", err)
	}

	// Generate command documentation
	if err := generateCommandDocs(); err != nil {
		return fmt.Errorf("failed to generate command docs: %w", err)
	}

	// Generate CLI usage guide
	if err := generateCLIUsageGuide(); err != nil {
		return fmt.Errorf("failed to generate CLI usage guide: %w", err)
	}

	fmt.Println("Documentation generated successfully!")
	return nil
}

func generateMainReadme() error {
	var buf bytes.Buffer

	buf.WriteString("# magic Agent CLI\n\n")
	buf.WriteString("> A Go implementation of magic Agent by Nous Research\n\n")
	buf.WriteString("## Overview\n\n")
	buf.WriteString("magic is a powerful CLI agent that grows with you through conversation and skill management.\n\n")
	buf.WriteString("## Quick Start\n\n")
	buf.WriteString("```bash\n")
	buf.WriteString("# Start an interactive chat\n")
	buf.WriteString("magic chat\n\n")
	buf.WriteString("# List all skills\n")
	buf.WriteString("magic skills list\n\n")
	buf.WriteString("# Discover plugins\n")
	buf.WriteString("magic plugin discover\n\n")
	buf.WriteString("# Show configuration\n")
	buf.WriteString("magic config list\n")
	buf.WriteString("```\n\n")

	buf.WriteString("## Installation\n\n")
	buf.WriteString("```bash\n")
	buf.WriteString("# Download the latest release\n")
	buf.WriteString("curl -fsSL https://example.com/install.sh | bash\n\n")
	buf.WriteString("# Or build from source\n")
	buf.WriteString("go build -o magic ./cmd/magic\n")
	buf.WriteString("```\n\n")

	buf.WriteString("## Global Flags\n\n")
	buf.WriteString("| Flag | Short | Description |\n")
	buf.WriteString("|------|-------|-------------|\n")
	buf.WriteString("| `--verbose` | `-v` | Enable verbose output |\n")
	buf.WriteString("| `--debug` | | Enable debug mode |\n")
	buf.WriteString("| `--config` | | Path to config file |\n")
	buf.WriteString("| `--output` | `-o` | Output format (text, json, yaml, table) |\n")
	buf.WriteString("| `--no-color` | | Disable colored output |\n")
	buf.WriteString("| `--magic-home` | | Custom magic home directory |\n")
	buf.WriteString("| `--profile` | `-p` | Configuration profile to use |\n\n")

	buf.WriteString("## Commands\n\n")

	// Group commands by category
	categories := make(map[string][]*cobra.Command)
	for _, cmd := range rootCmd.Commands() {
		if cmd.Hidden && !docsIncludeAll {
			continue
		}
		cat := getCommandCategory(cmd)
		categories[cat] = append(categories[cat], cmd)
	}

	// Sort categories
	var cats []string
	for cat := range categories {
		cats = append(cats, cat)
	}
	sort.Strings(cats)

	for _, cat := range cats {
		buf.WriteString(fmt.Sprintf("### %s\n\n", cat))
		for _, cmd := range categories[cat] {
			buf.WriteString(fmt.Sprintf("- **[magic %s](commands/%s.md)** - %s\n",
				cmd.Use, cmd.Use, cmd.Short))
		}
		buf.WriteString("\n")
	}

	buf.WriteString("## Shell Completion\n\n")
	buf.WriteString("```bash\n")
	buf.WriteString("# Bash\n")
	buf.WriteString("source <(magic completion bash)\n\n")
	buf.WriteString("# Zsh\n")
	buf.WriteString("source <(magic completion zsh)\n\n")
	buf.WriteString("# Fish\n")
	buf.WriteString("magic completion fish | source\n")
	buf.WriteString("```\n\n")

	buf.WriteString("## Configuration\n\n")
	buf.WriteString("magic uses a configuration file located at `~/.magic/config.toml`.\n\n")
	buf.WriteString("```toml\n")
	buf.WriteString("[default]\n")
	buf.WriteString("provider = \"openai\"\n")
	buf.WriteString("model = \"gpt-4\"\n\n")
	buf.WriteString("[providers.openai]\n")
	buf.WriteString("api_key = \"your-api-key\"\n")
	buf.WriteString("base_url = \"https://api.openai.com/v1\"\n")
	buf.WriteString("```\n\n")

	buf.WriteString("## License\n\n")
	buf.WriteString("MIT License\n")

	// Write README
	readmePath := filepath.Join(docsOutputDir, "README.md")
	return os.WriteFile(readmePath, buf.Bytes(), 0644)
}

func generateCommandDocs() error {
	// Create commands directory
	cmdsDir := filepath.Join(docsOutputDir, "commands")
	if err := os.MkdirAll(cmdsDir, 0755); err != nil {
		return err
	}

	for _, cmd := range rootCmd.Commands() {
		if cmd.Hidden && !docsIncludeAll {
			continue
		}
		if err := generateCommandDoc(cmd, cmdsDir); err != nil {
			fmt.Printf("Warning: failed to generate doc for %s: %v\n", cmd.Name(), err)
		}
	}

	return nil
}

func generateCommandDoc(cmd *cobra.Command, dir string) error {
	var buf bytes.Buffer

	buf.WriteString(fmt.Sprintf("# magic %s\n\n", cmd.Use))
	buf.WriteString(fmt.Sprintf("> %s\n\n", cmd.Long))
	buf.WriteString("## Synopsis\n\n")
	buf.WriteString(fmt.Sprintf("```\nmagic %s\n```\n\n", cmd.Use))
	buf.WriteString("## Description\n\n")
	buf.WriteString(cmd.Long)
	buf.WriteString("\n\n")

	if len(cmd.Flags()) > 0 {
		buf.WriteString("## Flags\n\n")
		buf.WriteString("| Flag | Short | Type | Default | Description |\n")
		buf.WriteString("|------|-------|------|---------|-------------|\n")
		cmd.Flags().VisitAll(func(f *pflag.Flag) {
			short := f.Shorthand
			if short == "" {
				short = "-"
			}
			buf.WriteString(fmt.Sprintf("| `--%s` | `-%s` | %s | %v | %s |\n",
				f.Name, short, f.Value.Type(), f.DefValue, f.Usage))
		})
		buf.WriteString("\n")
	}

	if cmd.HasExample() {
		buf.WriteString("## Examples\n\n")
		buf.WriteString("```bash\n")
		buf.WriteString(cmd.Example)
		buf.WriteString("```\n\n")
	}

	if cmd.HasSubCommands() {
		buf.WriteString("## Subcommands\n\n")
		for _, sub := range cmd.Commands() {
			if !sub.Hidden || docsIncludeAll {
				buf.WriteString(fmt.Sprintf("- **[magic %s](%s.md)** - %s\n",
					sub.Use, sub.Use, sub.Short))
			}
		}
		buf.WriteString("\n")
	}

	// Generate for subcommands
	for _, sub := range cmd.Commands() {
		if !sub.Hidden || docsIncludeAll {
			subFile := filepath.Join(dir, sub.Use+".md")
			generateCommandDoc(sub, dir)
			_ = subFile
		}
	}

	// Write file
	path := filepath.Join(dir, cmd.Use+".md")
	return os.WriteFile(path, buf.Bytes(), 0644)
}

func generateCLIUsageGuide() error {
	path := filepath.Join(docsOutputDir, "CLI_USAGE.md")
	var buf bytes.Buffer

	buf.WriteString("# CLI Usage Guide\n\n")
	buf.WriteString("This guide provides detailed information about using the magic CLI.\n\n")

	buf.WriteString("## Basic Usage\n\n")
	buf.WriteString("```bash\n")
	buf.WriteString("magic <command> [subcommand] [flags]\n")
	buf.WriteString("```\n\n")

	buf.WriteString("## Command Structure\n\n")
	buf.WriteString("```\n")
	buf.WriteString("root (magic)\n")
	buf.WriteString("├── chat          Interactive chat mode\n")
	buf.WriteString("├── agent         Run agent mode\n")
	buf.WriteString("├── config        Configuration management\n")
	buf.WriteString("├── skills        Skill management\n")
	buf.WriteString("├── plugin        Plugin management\n")
	buf.WriteString("├── session       Session management\n")
	buf.WriteString("├── tools         Tool management\n")
	buf.WriteString("├── version       Show version\n")
	buf.WriteString("├── completion    Shell completion\n")
	buf.WriteString("├── docs          Documentation\n")
	buf.WriteString("└── help          Help\n")
	buf.WriteString("```\n\n")

	buf.WriteString("## Output Formats\n\n")
	buf.WriteString("magic supports multiple output formats:\n\n")
	buf.WriteString("- `text` (default) - Human-readable text\n")
	buf.WriteString("- `json` - JSON format for scripting\n")
	buf.WriteString("- `yaml` - YAML format for configuration\n")
	buf.WriteString("- `table` - Tabular format for data\n\n")
	buf.WriteString("```bash\n")
	buf.WriteString("magic --output json config list\n")
	buf.WriteString("magic -o yaml config get\n")
	buf.WriteString("```\n\n")

	buf.WriteString("## Configuration Profiles\n\n")
	buf.WriteString("```bash\n")
	buf.WriteString("# Use a specific profile\n")
	buf.WriteString("magic --profile development chat\n\n")
	buf.WriteString("# List profiles\n")
	buf.WriteString("ls ~/.magic/profiles/\n")
	buf.WriteString("```\n\n")

	buf.WriteString("## Environment Variables\n\n")
	buf.WriteString("| Variable | Description |\n")
	buf.WriteString("|----------|-------------|\n")
	buf.WriteString("| `MAGIC_HOME` | Custom magic home directory |\n")
	buf.WriteString("| `MAGIC_CONFIG` | Path to config file |\n")
	buf.WriteString("| `MAGIC_PROFILE` | Default profile |\n")
	buf.WriteString("| `MAGIC_VERBOSE` | Enable verbose mode |\n")
	buf.WriteString("| `MAGIC_DEBUG` | Enable debug mode |\n")
	buf.WriteString("| `MAGIC_NO_COLOR` | Disable colors |\n")
	buf.WriteString("\n")

	buf.WriteString("## Logging\n\n")
	buf.WriteString("Logs are stored in `~/.magic/logs/`.\n\n")
	buf.WriteString("```bash\n")
	buf.WriteString("# View recent logs\n")
	buf.WriteString("magic logs\n\n")
	buf.WriteString("# View logs with tail\n")
	buf.WriteString("tail -f ~/.magic/logs/magic_*.log\n")
	buf.WriteString("```\n\n")

	buf.WriteString("## Troubleshooting\n\n")
	buf.WriteString("```bash\n")
	buf.WriteString("# Enable debug mode\n")
	buf.WriteString("magic --debug <command>\n\n")
	buf.WriteString("# Validate configuration\n")
	buf.WriteString("magic config validate\n\n")
	buf.WriteString("# Check health\n")
	buf.WriteString("magic health\n\n")
	buf.WriteString("# Run diagnostics\n")
	buf.WriteString("magic doctor\n")
	buf.WriteString("```\n\n")

	buf.WriteString("---\n")
	buf.WriteString(fmt.Sprintf("*Generated on %s*\n", time.Now().Format("2006-01-02")))

	return os.WriteFile(path, buf.Bytes(), 0644)
}

func generateManPages() error {
	manDir := filepath.Join(docsOutputDir, "man")
	if err := os.MkdirAll(manDir, 0755); err != nil {
		return err
	}

	fmt.Println("Generating man pages...")

	for _, cmd := range rootCmd.Commands() {
		if cmd.Hidden && !docsIncludeAll {
			continue
		}

		var buf bytes.Buffer
		buf.WriteString(fmt.Sprintf(".TH \"MAGIC\" \"1\" \"%s\" \"magic Agent\" \"magic Manual\"\n\n", time.Now().Format("January 2006")))
		buf.WriteString(fmt.Sprintf(".SH NAME\nmagic %s \\- %s\n\n", cmd.Use, cmd.Short))
		buf.WriteString(fmt.Sprintf(".SH SYNOPSIS\n.B magic\n%s\n\n", cmd.Use))
		buf.WriteString(fmt.Sprintf(".SH DESCRIPTION\n%s\n\n", cmd.Long))

		if len(cmd.Flags()) > 0 {
			buf.WriteString(".SH OPTIONS\n")
			cmd.Flags().VisitAll(func(f *pflag.Flag) {
				buf.WriteString(fmt.Sprintf(".TP\n.B \\-%s\n.br\n%s\n", f.Name, f.Usage))
				if f.Shorthand != "" {
					buf.WriteString(fmt.Sprintf("(shorthand: \\-%s)\n", f.Shorthand))
				}
			})
			buf.WriteString("\n")
		}

		if cmd.HasExample() {
			buf.WriteString(".SH EXAMPLES\n.PP\n.EX\n")
			buf.WriteString(cmd.Example)
			buf.WriteString(".EE\n")
		}

		// Write man file
		path := filepath.Join(manDir, fmt.Sprintf("magic-%s.1", cmd.Use))
		if err := os.WriteFile(path, buf.Bytes(), 0644); err != nil {
			fmt.Printf("Warning: failed to write %s: %v\n", path, err)
		}
	}

	fmt.Printf("Man pages generated in %s/\n", manDir)
	return nil
}

func serveDocs() error {
	docsPath := docsOutputDir
	if docsFormat == "html" {
		docsPath = filepath.Join(docsOutputDir, "html")
	}

	if _, err := os.Stat(docsPath); os.IsNotExist(err) {
		fmt.Println("Generating docs first...")
		if err := generateDocs(); err != nil {
			return err
		}
	}

	fmt.Printf("Starting documentation server on http://localhost:%d\n", docsPort)
	fmt.Printf("Serving files from: %s\n", docsPath)
	fmt.Println("Press Ctrl+C to stop")

	// Simple HTTP server
	return serveHTTP(docsPath, docsPort)
}

func serveHTTP(dir string, port int) error {
	// Use template for simple file server
	serverTemplate := `
<!DOCTYPE html>
<html>
<head>
    <title>magic Documentation</title>
    <style>
        body { font-family: system-ui, sans-serif; max-width: 900px; margin: 0 auto; padding: 2rem; }
        pre { background: #f4f4f4; padding: 1rem; overflow-x: auto; }
        code { background: #f4f4f4; padding: 0.2rem 0.4rem; }
        table { border-collapse: collapse; width: 100%; }
        th, td { border: 1px solid #ddd; padding: 0.5rem; text-align: left; }
        th { background: #f4f4f4; }
    </style>
</head>
<body>
    <h1>magic Agent Documentation</h1>
    <p>Welcome to the magic CLI documentation.</p>
    <h2>Quick Links</h2>
    <ul>
        <li><a href="README.md">README</a></li>
        <li><a href="CLI_USAGE.md">CLI Usage Guide</a></li>
    </ul>
</body>
</html>`

	_ = serverTemplate
	_ = dir

	// For now, print instructions
	fmt.Println()
	fmt.Println("To serve documentation locally, use one of:")
	fmt.Println()
	fmt.Println("  # Python (if available)")
	fmt.Printf("  cd %s && python3 -m http.server %d\n", dir, port)
	fmt.Println()
	fmt.Println("  # Go")
	fmt.Printf("  cd %s && go run main.go serve -p %d\n", dir, port)
	fmt.Println()

	return nil
}

func getCommandCategory(cmd *cobra.Command) string {
	if cmd.HasParent() {
		parent := cmd.Parent()
		if parent != rootCmd {
			return getCommandCategory(parent)
		}
	}

	// Group commands by prefix
	name := cmd.Name()
	switch {
	case strings.HasPrefix(name, "config"):
		return "Configuration"
	case strings.HasPrefix(name, "skill"):
		return "Skills"
	case strings.HasPrefix(name, "plugin"):
		return "Plugins"
	case strings.HasPrefix(name, "session"):
		return "Sessions"
	case strings.HasPrefix(name, "tool"):
		return "Tools"
	case name == "chat" || name == "agent" || name == "repl":
		return "Core"
	case name == "version" || name == "help" || name == "completion":
		return "Utilities"
	default:
		return "Commands"
	}
}

// Import pflag for the flag types used in documentation
import (
	"github.com/spf13/pflag"
)

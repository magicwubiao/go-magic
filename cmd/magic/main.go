package main

import (
	"fmt"
	"os"
	"text/template"

	"github.com/spf13/cobra"
)

// Global flags
var (
	flagVerbose    bool
	flagDebug      bool
	flagConfig     string
	flagOutput     string
	flagNoColor    bool
	flagMagicHome  string
	flagProfile    string
	flagVersion    bool
)

var rootCmd = &cobra.Command{
	Use:   "magic",
	Short: "magic Agent - The agent that grows with you",
	Long: `magic Agent - A Go implementation of magic Agent by Nous Research

A powerful CLI agent that grows with you through conversation and skill management.

Quick Start:
  magic chat                    Start an interactive chat
  magic skills list             List all available skills
  magic plugin discover         Discover available plugins
  magic config list             Show current configuration

Global Flags:
  --verbose, -v    Enable verbose logging
  --debug           Enable debug mode
  --config          Path to config file
  --output, -o      Output format (text, json, yaml)
  --no-color        Disable colored output
  --magic-home      Custom magic home directory

Examples:
  magic chat -v                    Start chat with verbose logging
  magic --output json config list  List config in JSON format
  magic --debug run "hello world"  Run task with debug enabled
  magic --config /path/to/config version  Use custom config file`,
	SilenceUsage:  true,
	SilenceErrors: true,
}

// helpTemplate is the custom help template for magic
const helpTemplate = `
{{bold .Name}}{{bold " - " .Short}}{{bold "\n"}}{{if .Long}}{{.Long | trim}}{{else}}{{.Short | trim}}{{end}}
{{if or .Runnable .HasSubCommands}}
{{bold "\nUsage:"}}{{if .Runnable}}
  {{.UseLine}}{{end}}{{if .HasSubCommands}}
  {{.CommandPath}} <command>{{end}}{{end}}

{{if .HasSubCommands}}{{bold "\nAvailable Commands:"}}
{{range .Commands}}{{if .IsAvailableCommand}}
  {{rpad .Name .NamePadding}} {{.Short}}{{end}}{{end}}{{end}}

{{if .HasLocalFlags}}{{bold "\nFlags:"}}
{{.LocalFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}
{{if .HasInheritedFlags}}{{bold "\nGlobal Flags:"}}
{{.InheritedFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}
{{if .HasSubCommands}}
{{bold "\nUse \"{{.CommandPath}} [command] --help\" for more information about a command."}}{{end}}
{{if .HasExample}}{{bold "\nExamples:"}}
{{.Example}}{{end}}
{{if .HasSubCommands}}{{range .Commands}}{{if .IsAdditionalHelpTopicCommand}}
{{.CmdPath}}{{" "}}{{.UsageLine}}{{end}}{{end}}{{end}}
`

// usageTemplate is the custom usage template
const usageTemplate = `
{{bold "Usage:"}} {{.UseLine}}{{if .HasFlags}} [flags]{{end}}{{if .HasSubCommands}} <command>{{end}}

{{if .HasSubCommands}}{{bold "\nAvailable Commands:"}}
{{range .Commands}}{{if .IsAvailableCommand}}
  {{rpad .Name .NamePadding}} {{.Short}}{{end}}{{end}}{{end}}
{{if gt (len .Aliases) 0}}{{bold "\nAlso Known As:"}}
  {{.AliasesAndNames}}{{end}}{{if .HasFlags}}{{bold "\nFlags:"}}
{{.Flags.FlagUsages | trimTrailingWhitespaces}}{{end}}
`

func init() {
	// Define template functions
	funcMap := template.FuncMap{
		"bold": func(s string) string {
			if flagNoColor {
				return s
			}
			return "\033[1m" + s + "\033[0m"
		},
		"rpad": func(s string, pad int) string {
			padding := pad - len(s)
			if padding <= 0 {
				return s
			}
			return s + repeat(" ", padding)
		},
		"trim": func(s string) string {
			return trimSpace(s)
		},
		"trimTrailingWhitespaces": trimSpace,
	}

	// Register global flags
	rootCmd.PersistentFlags().BoolVarP(&flagVerbose, "verbose", "v", false, "Enable verbose output")
	rootCmd.PersistentFlags().BoolVar(&flagDebug, "debug", false, "Enable debug mode")
	rootCmd.PersistentFlags().StringVar(&flagConfig, "config", "", "Path to config file")
	rootCmd.PersistentFlags().StringVarP(&flagOutput, "output", "o", "text", "Output format (text, json, yaml, table)")
	rootCmd.PersistentFlags().BoolVar(&flagNoColor, "no-color", false, "Disable colored output")
	rootCmd.PersistentFlags().StringVar(&flagMagicHome, "magic-home", "", "Custom magic home directory")
	rootCmd.PersistentFlags().StringVarP(&flagProfile, "profile", "p", "", "Configuration profile to use")

	// Add help command customization with template functions
	helpTmpl := template.Must(template.New("help").Funcs(funcMap).Parse(helpTemplate))
	rootCmd.SetHelpTemplate(helpTmpl.Root.String())

	usageTmpl := template.Must(template.New("usage").Funcs(funcMap).Parse(usageTemplate))
	rootCmd.SetUsageTemplate(usageTmpl.Root.String())

	// PersistentPreRun hook for all commands
	rootCmd.PersistentPreRun = func(cmd *cobra.Command, args []string) {
		// Override verbose if debug is set
		if flagDebug {
			flagVerbose = true
		}
	}

	// Version flag
	rootCmd.Flags().BoolVar(&flagVersion, "version", false, "Show version information")
}

func main() {
	// Check for version flag first
	if len(os.Args) > 1 && os.Args[1] == "--version" {
		os.Args[1] = "version"
	}

	initLogging()
	defer func() {
		if logFile != nil {
			logFile.Close()
		}
	}()

	// Execute with error handling
	if err := rootCmd.Execute(); err != nil {
		// Output error in appropriate format
		switch flagOutput {
		case "json":
			fmt.Printf(`{"error": %q}`, err.Error())
		case "yaml":
			fmt.Printf("error: %s\n", err.Error())
		default:
			if !flagNoColor {
				fmt.Printf("\033[1;31mError:\033[0m %v\n", err)
			} else {
				fmt.Printf("Error: %v\n", err)
			}
		}
		os.Exit(1)
	}
}

func repeat(s string, count int) string {
	result := ""
	for i := 0; i < count; i++ {
		result += s
	}
	return result
}

func trimSpace(s string) string {
	// Simple trim implementation
	i := 0
	for i < len(s) && (s[i] == ' ' || s[i] == '\t' || s[i] == '\n' || s[i] == '\r') {
		i++
	}
	j := len(s)
	for j > i && (s[j-1] == ' ' || s[j-1] == '\t' || s[j-1] == '\n' || s[j-1] == '\r') {
		j--
	}
	return s[i:j]
}

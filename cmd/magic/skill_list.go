package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/magicwubiao/go-magic/internal/skills"
	"github.com/magicwubiao/go-magic/internal/skills/importer"
)

var (
	listFormat    string
	listFilter    string
	listSource    string
	listShowTools bool
	listJSON      bool
)

var skillListCmd = &cobra.Command{
	Use:   "list",
	Short: "List installed skills",
	Long: `List all installed skills with optional filtering and formatting.

Skills are loaded from:
  - Built-in skills: bundled with the application
  - Global skills: ~/.magic/skills/
  - Workspace skills: ./skills/ or .magic/skills/

Examples:
  # List all skills
  magic skill list

  # List skills in JSON format
  magic skill list --format json

  # List only global skills
  magic skill list --source global

  # Filter skills by name
  magic skill list --filter api

  # Show skill tools
  magic skill list --show-tools`,
	Run: runSkillList,
}

func init() {
	skillListCmd.Flags().StringVar(&listFormat, "format", "table", "Output format: table, list, json")
	skillListCmd.Flags().StringVar(&listFilter, "filter", "", "Filter skills by name or tag")
	skillListCmd.Flags().StringVar(&listSource, "source", "", "Filter by source: builtin, global, local, imported")
	skillListCmd.Flags().BoolVar(&listShowTools, "show-tools", false, "Show required tools for each skill")
	skillListCmd.Flags().BoolVarP(&listJSON, "json", "j", false, "Output in JSON format")

	skillCmd.AddCommand(skillListCmd)
	skillListCmd.AddCommand(skillAvailableCmd)
}

func runSkillList(cmd *cobra.Command, args []string) {
	mgr, err := skills.NewManager()
	if err != nil {
		fmt.Printf("Failed to load skills: %v\n", err)
		os.Exit(1)
	}

	skillList := mgr.List()

	// Apply filters
	if listFilter != "" {
		skillList = filterSkills(skillList, listFilter)
	}
	if listSource != "" {
		skillList = filterBySource(skillList, listSource)
	}

	// Output
	if listJSON {
		outputJSON(skillList)
	} else if listFormat == "json" {
		outputJSON(skillList)
	} else if listFormat == "list" {
		outputList(skillList)
	} else {
		outputTable(skillList)
	}
}

func filterSkills(list []*skills.Skill, filter string) []*skills.Skill {
	filter = strings.ToLower(filter)
	var result []*skills.Skill

	for _, s := range list {
		name := strings.ToLower(s.Name)
		desc := strings.ToLower(s.Description)

		// Check name and description
		if strings.Contains(name, filter) || strings.Contains(desc, filter) {
			result = append(result, s)
			continue
		}

		// Check tags
		for _, tag := range s.Tags {
			if strings.Contains(strings.ToLower(tag), filter) {
				result = append(result, s)
				break
			}
		}
	}

	return result
}

func filterBySource(list []*skills.Skill, source string) []*skills.Skill {
	source = strings.ToLower(source)
	var result []*skills.Skill

	for _, s := range list {
		if strings.ToLower(s.Source) == source {
			result = append(result, s)
		}
	}

	return result
}

func outputTable(list []*skills.Skill) {
	if len(list) == 0 {
		fmt.Println("No skills found.")
		showSkillsLocations()
		return
	}

	fmt.Printf("Found %d skills:\n\n", len(list))

	// Group by source
	bySource := make(map[string][]*skills.Skill)
	for _, s := range list {
		source := s.Source
		if source == "" {
			source = "local"
		}
		bySource[source] = append(bySource[source], s)
	}

	// Sort sources
	var sources []string
	for source := range bySource {
		sources = append(sources, source)
	}
	sort.Strings(sources)

	for _, source := range sources {
		skills := bySource[source]
		fmt.Printf("## %s (%d)\n", strings.ToUpper(source), len(skills))
		fmt.Println(strings.Repeat("-", 40))

		if listShowTools {
			// Detailed table with tools
			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintf(w, "  %-25s %-40s %s\n", "NAME", "DESCRIPTION", "TOOLS")
			fmt.Fprintf(w, "  %-25s %-40s %s\n", strings.Repeat("-", 25), strings.Repeat("-", 40), strings.Repeat("-", 20))

			for _, s := range skills {
				tags := ""
				if len(s.Tags) > 0 {
					tags = "[" + strings.Join(s.Tags, ", ") + "]"
				}
				desc := s.Description
				if len(desc) > 38 {
					desc = desc[:35] + "..."
				}
				tools := strings.Join(s.Tools, ", ")
				if len(tools) > 18 {
					tools = tools[:15] + "..."
				}
				fmt.Fprintf(w, "  %-25s %-40s %s %s\n", s.Name, desc, tools, tags)
			}
			w.Flush()
		} else {
			// Simple list
			for _, s := range skills {
				tags := ""
				if len(s.Tags) > 0 {
					tags = fmt.Sprintf(" [%s]", strings.Join(s.Tags, ", "))
				}
				desc := s.Description
				if len(desc) > 50 {
					desc = desc[:47] + "..."
				}
				fmt.Printf("  • %s: %s%s\n", s.Name, desc, tags)
			}
		}
		fmt.Println()
	}
}

func outputList(list []*skills.Skill) {
	if len(list) == 0 {
		fmt.Println("No skills found.")
		return
	}

	for _, s := range list {
		fmt.Printf("%s\n", s.Name)
		if s.Description != "" {
			fmt.Printf("  %s\n", s.Description)
		}
		fmt.Println()
	}
}

func outputJSON(list []*skills.Skill) {
	type skillOutput struct {
		Name        string   `json:"name"`
		Description string   `json:"description"`
		Version     string   `json:"version,omitempty"`
		Author      string   `json:"author,omitempty"`
		Tags        []string `json:"tags,omitempty"`
		Tools       []string `json:"tools,omitempty"`
		Source      string   `json:"source"`
	}

	var output []skillOutput
	for _, s := range list {
		output = append(output, skillOutput{
			Name:        s.Name,
			Description: s.Description,
			Version:     s.Version,
			Author:      s.Author,
			Tags:        s.Tags,
			Tools:       s.Tools,
			Source:      s.Source,
		})
	}

	data, err := json.MarshalIndent(output, "", "  ")
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}

	fmt.Println(string(data))
}

func showSkillsLocations() {
	home, _ := os.UserHomeDir()

	fmt.Println("Skills directories:")
	fmt.Printf("  ~/.magic/skills/ (global)\n")
	fmt.Printf("  ./skills/ (workspace)\n")
	fmt.Printf("  .magic/skills/ (workspace)\n")

	// Check if global skills directory exists
	globalDir := filepath.Join(home, ".magic", "skills")
	if _, err := os.Stat(globalDir); os.IsNotExist(err) {
		fmt.Printf("\nCreate global skills directory:\n")
		fmt.Printf("  mkdir -p %s\n", globalDir)
	}
}

// ShowAvailableSkills shows skills available in a directory (for import preview)
var skillAvailableCmd = &cobra.Command{
	Use:   "available [path]",
	Short: "List skills available in a directory",
	Long: `List skills available in a directory for import.

Shows all skill directories with SKILL.md files, including their format
(OpenClaw, Hermes, or Magic) and basic metadata.

Examples:
  # List available skills in ./skills
  magic skill available ./skills

  # List in current directory
  magic skill available`,
	Args: cobra.RangeArgs(0, 1),
	Run:  runSkillAvailable,
}

func runSkillAvailable(cmd *cobra.Command, args []string) {
	path := "./skills"
	if len(args) > 0 {
		path = args[0]
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		fmt.Printf("Error: invalid path: %v\n", err)
		os.Exit(1)
	}

	info, err := os.Stat(absPath)
	if os.IsNotExist(err) {
		fmt.Printf("Error: path not found: %s\n", absPath)
		os.Exit(1)
	}

	if !info.IsDir() {
		fmt.Printf("Error: %s is not a directory\n", absPath)
		os.Exit(1)
	}

	imp := importer.NewImporter(nil)
	skills, err := imp.ListAvailableSkills(absPath)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}

	if len(skills) == 0 {
		fmt.Printf("No skills found in %s\n", absPath)
		return
	}

	fmt.Printf("Available skills in %s:\n\n", absPath)

	for _, s := range skills {
		formatBadge := s.Format
		if s.Format == "openclaw" {
			formatBadge = "openclaw"
		} else if s.Format == "hermes" {
			formatBadge = "hermes"
		}

		fmt.Printf("  • %s [%s]\n", s.Name, formatBadge)
		if s.Description != "" {
			desc := s.Description
			if len(desc) > 60 {
				desc = desc[:57] + "..."
			}
			fmt.Printf("    %s\n", desc)
		}
	}

	fmt.Printf("\nImport with: magic skill import <path> --recursive\n")
}

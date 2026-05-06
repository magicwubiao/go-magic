package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

// Migration source type
type migrateSource string

const (
	sourceOpenClaw migrateSource = "openclaw"
	sourceHermes   migrateSource = "hermes"
)

// Home directory detection (cross-platform)
func getHomeDir() string {
	if home := os.Getenv("HOME"); home != "" {
		return home
	}
	return os.Getenv("USERPROFILE")
}

func (s migrateSource) String() string {
	return string(s)
}

func (s migrateSource) dir() string {
	home := getHomeDir()
	switch s {
	case sourceHermes:
		// Check ~/.hermes first, then ~/.config/hermes
		dir := filepath.Join(home, ".hermes")
		if _, err := os.Stat(dir); err == nil {
			return dir
		}
		return filepath.Join(home, ".config", "hermes")
	default:
		return filepath.Join(home, ".openclaw")
	}
}

func (s migrateSource) DisplayName() string {
	switch s {
	case sourceHermes:
		return "Hermes Agent"
	default:
		return "OpenClaw"
	}
}

// HermsConfig represents Hermes config.yaml structure
type CortexConfig struct {
	Version string `yaml:"version"`
	Name    string `yaml:"name"`
	Model   struct {
		Provider string `yaml:"provider"`
		Model    string `yaml:"model"`
	} `yaml:"model"`
	Skills []string `yaml:"skills"`
}

// HermesUser represents Hermes user.json structure
type HermesUser struct {
	Name     string `json:"name"`
	Email    string `json:"email"`
	Settings struct {
		Theme        string `json:"theme"`
		Language     string `json:"language"`
		Timezone     string `json:"timezone"`
		Notifications struct {
			Email bool `json:"email"`
			Push  bool `json:"push"`
		} `json:"notifications"`
	} `json:"settings"`
}

// HermesMemory represents Hermes memory.json structure
type HermesMemory struct {
	Entries []struct {
		Type      string `json:"type"`
		Content   string `json:"content"`
		Timestamp string `json:"timestamp"`
		Tags      []string `json:"tags"`
	} `json:"entries"`
}

// Convert Hermes memory to markdown format
func convertMemoryToMarkdown(memory *HermesMemory) string {
	var md string
	md += "# Memory\n\n"
	md += "Auto-migrated from Hermes Agent\n\n"
	
	for _, entry := range memory.Entries {
		md += fmt.Sprintf("## [%s] %s\n\n", entry.Type, entry.Timestamp)
		md += entry.Content + "\n\n"
		if len(entry.Tags) > 0 {
			md += fmt.Sprintf("Tags: %v\n\n", entry.Tags)
		}
	}
	return md
}

// Convert Hermes user.json to markdown format
func convertUserToMarkdown(user *HermesUser) string {
	var md string
	md += "# User Profile\n\n"
	md += "Auto-migrated from Hermes Agent\n\n"
	md += fmt.Sprintf("- **Name**: %s\n", user.Name)
	md += fmt.Sprintf("- **Email**: %s\n", user.Email)
	md += "\n## Settings\n\n"
	md += fmt.Sprintf("- **Theme**: %s\n", user.Settings.Theme)
	md += fmt.Sprintf("- **Language**: %s\n", user.Settings.Language)
	md += fmt.Sprintf("- **Timezone**: %s\n", user.Settings.Timezone)
	md += fmt.Sprintf("- **Email Notifications**: %v\n", user.Settings.Notifications.Email)
	md += fmt.Sprintf("- **Push Notifications**: %v\n", user.Settings.Notifications.Push)
	return md
}

// Detect available migration source
func detectSource() migrateSource {
	// Check OpenClaw first
	openclawDir := filepath.Join(getHomeDir(), ".openclaw")
	if _, err := os.Stat(openclawDir); err == nil {
		return sourceOpenClaw
	}
	
	// Check Hermes (~/.hermes or ~/.config/hermes)
	hermesDir1 := filepath.Join(getHomeDir(), ".hermes")
	hermesDir2 := filepath.Join(getHomeDir(), ".config", "hermes")
	if _, err := os.Stat(hermesDir1); err == nil {
		return sourceHermes
	}
	if _, err := os.Stat(hermesDir2); err == nil {
		return sourceHermes
	}
	
	return sourceOpenClaw // Default fallback
}

var dataCmd = &cobra.Command{
	Use:   "data",
	Short: "Data migration utilities",
}

var dataMigrateCmd = &cobra.Command{
	Use:   "migrate",
	Short: "Migrate settings from OpenClaw or Hermes Agent",
	Run:   runDataMigrate,
}

var fromSource migrateSource

func init() {
	dataMigrateCmd.Flags().Bool("dry-run", false, "Preview what would be migrated")
	dataMigrateCmd.Flags().String("preset", "full", "Migration preset: full, user-data")
	dataMigrateCmd.Flags().Bool("overwrite", false, "Overwrite existing files")
	dataMigrateCmd.Flags().VarP(&fromSource, "from", "f", "Migration source: openclaw, hermes (auto-detected by default)")

	// Set default value after declaration
	fromSource = sourceOpenClaw

	dataCmd.AddCommand(dataMigrateCmd)
	rootCmd.AddCommand(dataCmd)
}

func runDataMigrate(cmd *cobra.Command, args []string) {
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	preset, _ := cmd.Flags().GetString("preset")
	overwrite, _ := cmd.Flags().GetBool("overwrite")

	// Check if --from flag was explicitly set
	flagChanged := cmd.Flags().Changed("from")
	
	// Auto-detect source if not explicitly specified
	if !flagChanged {
		fromSource = detectSource()
	}

	fmt.Printf("%s Migration Tool\n", fromSource.DisplayName())
	fmt.Println("====================")
	fmt.Println()

	// Check if source directory exists
	srcDir := fromSource.dir()
	if _, err := os.Stat(srcDir); os.IsNotExist(err) {
		fmt.Printf("Source directory not found: %s\n", srcDir)
		if !flagChanged {
			fmt.Println("No supported migration source detected.")
			fmt.Println("Please ensure either OpenClaw (~/.openclaw) or Hermes Agent (~/.hermes or ~/.config/hermes) is installed.")
		}
		fmt.Println("Nothing to migrate.")
		return
	}

	fmt.Printf("Found %s directory: %s\n", fromSource.DisplayName(), srcDir)
	if flagChanged {
		fmt.Printf("Source: explicitly specified via --from=%s\n", fromSource)
	} else {
		fmt.Printf("Source: auto-detected (use --from to override)\n")
	}
	fmt.Printf("Migration preset: %s\n", preset)
	fmt.Printf("Dry run: %v\n", dryRun)
	fmt.Printf("Overwrite: %v\n", overwrite)
	fmt.Println()

	magicDir := filepath.Join(getHomeDir(), ".magic")
	os.MkdirAll(magicDir, 0755)

	// Items to migrate
	type migrateItem struct {
		name string
		src  string
		dst  string
		transform func(src string) (string, error) // For content transformation
	}

	items := []migrateItem{}

	switch fromSource {
	case sourceHermes:
		// Hermes-specific migration items
		items = buildHermesMigrateItems(srcDir, magicDir)
	default:
		// OpenClaw migration items
		items = buildOpenClawMigrateItems(srcDir, magicDir)
	}

	// Filter by preset
	if preset == "user-data" {
		filtered := []migrateItem{}
		for _, item := range items {
			// Only include user-related items
			if item.name == "SOUL.md (persona)" || 
			   item.name == "MEMORY.md" || 
			   item.name == "USER.md" ||
			   item.name == "persona.md (Hermes)" {
				filtered = append(filtered, item)
			}
		}
		items = filtered
	}

	for _, item := range items {
		fmt.Printf("[ ] %s\n", item.name)
		fmt.Printf("    From: %s\n", item.src)
		fmt.Printf("    To:   %s\n", item.dst)

		if dryRun {
			fmt.Println("    (dry run - not copied)")
			continue
		}

		// Check if source exists
		srcInfo, err := os.Stat(item.src)
		if os.IsNotExist(err) {
			fmt.Println("    Source not found, skipping.")
			continue
		}

		// Check if destination exists
		if _, err := os.Stat(item.dst); err == nil && !overwrite {
			fmt.Println("    Destination exists, use --overwrite to replace")
			continue
		}

		// Copy file or directory
		if srcInfo.IsDir() {
			if err := copyPath(item.src, item.dst); err != nil {
				fmt.Printf("    Error: %v\n", err)
			} else {
				fmt.Println("    ✓ Migrated!")
			}
		} else {
			// Handle content transformation if needed
			if item.transform != nil {
				content, err := item.transform(item.src)
				if err != nil {
					fmt.Printf("    Transform error: %v\n", err)
					continue
				}
				if err := os.WriteFile(item.dst, []byte(content), 0644); err != nil {
					fmt.Printf("    Error: %v\n", err)
				} else {
					fmt.Println("    ✓ Migrated (transformed)!")
				}
			} else {
				if err := copyFile(item.src, item.dst); err != nil {
					fmt.Printf("    Error: %v\n", err)
				} else {
					fmt.Println("    ✓ Migrated!")
				}
			}
		}
	}

	if dryRun {
		fmt.Println("\nDry run complete. Run without --dry-run to actually migrate.")
	} else {
		fmt.Println("\nMigration complete!")
	}
}

func buildOpenClawMigrateItems(srcDir, dstDir string) []migrateItem {
	return []migrateItem{
		{"SOUL.md (persona)", filepath.Join(srcDir, "SOUL.md"), filepath.Join(dstDir, "SOUL.md"), nil},
		{"MEMORY.md", filepath.Join(srcDir, "MEMORY.md"), filepath.Join(dstDir, "MEMORY.md"), nil},
		{"USER.md", filepath.Join(srcDir, "USER.md"), filepath.Join(dstDir, "USER.md"), nil},
		{"Config", filepath.Join(srcDir, "config.yaml"), filepath.Join(dstDir, "config.yaml"), nil},
		{"Skills", filepath.Join(srcDir, "skills"), filepath.Join(dstDir, "skills"), nil},
	}
}

func buildHermesMigrateItems(srcDir, dstDir string) []migrateItem {
	return []migrateItem{
		// Persona: persona.md -> SOUL.md
		{"persona.md (Hermes)", filepath.Join(srcDir, "persona.md"), filepath.Join(dstDir, "SOUL.md"), nil},
		
		// Memory: memory.json/memory.db -> MEMORY.md
		{"memory.json (Hermes)", filepath.Join(srcDir, "memory.json"), filepath.Join(dstDir, "MEMORY.md"), transformHermesMemory},
		
		// User: user.json -> USER.md
		{"user.json (Hermes)", filepath.Join(srcDir, "user.json"), filepath.Join(dstDir, "USER.md"), transformHermesUser},
		
		// Config: direct copy
		{"Config", filepath.Join(srcDir, "config.yaml"), filepath.Join(dstDir, "config.yaml"), nil},
		
		// Skills: directory copy
		{"Skills", filepath.Join(srcDir, "skills"), filepath.Join(dstDir, "skills"), nil},
		
		// Conversations: directory copy
		{"Conversations", filepath.Join(srcDir, "conversations"), filepath.Join(dstDir, "conversations"), nil},
	}
}

func transformHermesMemory(src string) (string, error) {
	data, err := os.ReadFile(src)
	if err != nil {
		return "", err
	}

	// Try to parse as JSON
	var memory HermesMemory
	if err := json.Unmarshal(data, &memory); err == nil {
		return convertMemoryToMarkdown(&memory), nil
	}

	// If not JSON, treat as plain text memory
	return "# Memory\n\nAuto-migrated from Hermes Agent\n\n" + string(data), nil
}

func transformHermesUser(src string) (string, error) {
	data, err := os.ReadFile(src)
	if err != nil {
		return "", err
	}

	// Try to parse as JSON
	var user HermesUser
	if err := json.Unmarshal(data, &user); err == nil {
		return convertUserToMarkdown(&user), nil
	}

	// If not JSON, treat as plain text
	return "# User Profile\n\nAuto-migrated from Hermes Agent\n\n" + string(data), nil
}

func copyPath(src, dst string) error {
	srcInfo, err := os.Stat(src)
	if err != nil {
		return err
	}

	if srcInfo.IsDir() {
		return copyDir(src, dst)
	}
	return copyFile(src, dst)
}

func copyFile(src, dst string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	dstFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer dstFile.Close()

	_, err = io.Copy(dstFile, srcFile)
	return err
}

func copyDir(src, dst string) error {
	os.MkdirAll(dst, 0755)

	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		srcPath := filepath.Join(src, entry.Name())
		dstPath := filepath.Join(dst, entry.Name())

		if entry.IsDir() {
			if err := copyDir(srcPath, dstPath); err != nil {
				return err
			}
		} else {
			if err := copyFile(srcPath, dstPath); err != nil {
				return err
			}
		}
	}
	return nil
}

// Unused but kept for compatibility if needed
func _parseCortexConfig(path string) (*CortexConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var config CortexConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, err
	}
	return &config, nil
}

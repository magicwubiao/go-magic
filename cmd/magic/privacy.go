package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/magicwubiao/go-magic/internal/privacy"
	"github.com/magicwubiao/go-magic/pkg/config"
)

var privacyCmd = &cobra.Command{
	Use:   "privacy",
	Short: "PII detection and redaction tools",
	Long: `Detect and redact Personally Identifiable Information (PII)
such as phone numbers, emails, ID cards, bank cards, and IP addresses.`,
}

var privacyRedactCmd = &cobra.Command{
	Use:   "redact [text]",
	Short: "Redact PII from text",
	Args:  cobra.RangeArgs(0, 1),
	Run:   runPrivacyRedact,
}

var privacyDetectCmd = &cobra.Command{
	Use:   "detect [text]",
	Short: "Detect PII in text without redacting",
	Args:  cobra.RangeArgs(0, 1),
	Run:   runPrivacyDetect,
}

var privacyAuditCmd = &cobra.Command{
	Use:   "audit",
	Short: "Show PII redaction audit log",
	Run:   runPrivacyAudit,
}

var privacyStatsCmd = &cobra.Command{
	Use:   "stats",
	Short: "Show PII statistics",
	Run:   runPrivacyStats,
}

var (
	privacyJSON    bool
	privacyEnable  bool
	privacyDisable bool
)

func init() {
	rootCmd.AddCommand(privacyCmd)
	privacyCmd.AddCommand(privacyRedactCmd)
	privacyCmd.AddCommand(privacyDetectCmd)
	privacyCmd.AddCommand(privacyAuditCmd)
	privacyCmd.AddCommand(privacyStatsCmd)

	privacyRedactCmd.Flags().BoolVarP(&privacyJSON, "json", "j", false, "Output as JSON")
	privacyRedactCmd.Flags().BoolVar(&privacyEnable, "enable", false, "Enable redaction")
	privacyRedactCmd.Flags().BoolVar(&privacyDisable, "disable", false, "Disable redaction")

	privacyDetectCmd.Flags().BoolVarP(&privacyJSON, "json", "j", false, "Output as JSON")
}

func runPrivacyRedact(cmd *cobra.Command, args []string) {
	r := privacy.DefaultRedactor()

	// Handle enable/disable flags
	if privacyEnable {
		r.Enable()
		fmt.Println("PII redaction enabled")
		return
	}

	if privacyDisable {
		r.Disable()
		fmt.Println("PII redaction disabled")
		return
	}

	// Get input text
	text := ""
	if len(args) > 0 {
		text = args[0]
	} else {
		// Read from stdin
		fmt.Println("Enter text to redact (Ctrl+D to finish):")
		var err error
		text, err = readStdin()
		if err != nil {
			fmt.Printf("Error reading input: %v\n", err)
			os.Exit(1)
		}
	}

	result, detected := r.RedactWithContext(text)

	if privacyJSON {
		output := map[string]interface{}{
			"original": text,
			"redacted": result,
			"detected": detected,
			"enabled":  r.IsEnabled(),
		}
		data, _ := json.MarshalIndent(output, "", "  ")
		fmt.Println(string(data))
	} else {
		fmt.Printf("Original: %s\n", text)
		fmt.Printf("Redacted: %s\n", result)
		if len(detected) > 0 {
			fmt.Printf("Detected: %s\n", strings.Join(detected, ", "))
		}
	}
}

func runPrivacyDetect(cmd *cobra.Command, args []string) {
	r := privacy.DefaultRedactor()

	// Get input text
	text := ""
	if len(args) > 0 {
		text = args[0]
	} else {
		// Read from stdin
		var err error
		text, err = readStdin()
		if err != nil {
			fmt.Printf("Error reading input: %v\n", err)
			os.Exit(1)
		}
	}

	matches := r.Detect(text)

	if privacyJSON {
		output := map[string]interface{}{
			"text":    text,
			"matches": matches,
			"count":   len(matches),
		}
		data, _ := json.MarshalIndent(output, "", "  ")
		fmt.Println(string(data))
	} else {
		if len(matches) == 0 {
			fmt.Println("No PII detected.")
			return
		}

		fmt.Printf("Found %d PII matches:\n\n", len(matches))

		// Group by type
		byType := make(map[string][]privacy.Match)
		for _, m := range matches {
			byType[m.Type] = append(byType[m.Type], m)
		}

		for piiType, matches := range byType {
			fmt.Printf("[%s] (%d found)\n", piiType, len(matches))
			for _, m := range matches {
				// Show context around match
				start := m.Start - 10
				if start < 0 {
					start = 0
				}
				end := m.End + 10
				if end > len(text) {
					end = len(text)
				}

				context := text[start:end]
				// Replace newlines for display
				context = strings.ReplaceAll(context, "\n", "\\n")
				fmt.Printf("  • %s\n", context)
				fmt.Printf("    Value: %s (pos: %d-%d)\n", m.Value, m.Start, m.End)
			}
			fmt.Println()
		}
	}
}

func runPrivacyAudit(cmd *cobra.Command, args []string) {
	r := privacy.DefaultRedactor()

	log := r.GetAuditLog()

	if len(log) == 0 {
		fmt.Println("No audit log entries.")
		return
	}

	fmt.Printf("PII Redaction Audit Log (%d entries)\n", len(log))
	fmt.Println("====================================")

	for i, entry := range log {
		fmt.Printf("%d. Detected: %s\n", i+1, strings.Join(entry.Detected, ", "))
		fmt.Printf("   Original: %s\n", truncateStr(entry.Original, 50))
		fmt.Printf("   Redacted: %s\n", truncateStr(entry.Redacted, 50))
		fmt.Println()
	}
}

func runPrivacyStats(cmd *cobra.Command, args []string) {
	r := privacy.DefaultRedactor()

	cfg, _ := config.Load()

	fmt.Println("PII Detection Configuration")
	fmt.Println("==========================")

	privCfg := privacy.DefaultConfig()
	if cfg != nil && cfg.Privacy != nil {
		privCfg = cfg.Privacy
	}

	fmt.Printf("Enabled: %v\n", privCfg.Enabled)
	fmt.Printf("Redact Phone: %v\n", privCfg.RedactPhone)
	fmt.Printf("Redact Email: %v\n", privCfg.RedactEmail)
	fmt.Printf("Redact ID Card: %v\n", privCfg.RedactIDCard)
	fmt.Printf("Redact Bank Card: %v\n", privCfg.RedactBankCard)
	fmt.Printf("Redact IP: %v\n", privCfg.RedactIP)
	fmt.Printf("Redact Address: %v\n", privCfg.RedactAddress)

	fmt.Println("\nConfigured Patterns:")
	for _, p := range r.GetPatterns() {
		fmt.Printf("  • %s\n", p)
	}

	fmt.Println("\nExample:")
	fmt.Println("  magic privacy redact 'My phone is 13812345678'")
	fmt.Println("  magic privacy detect 'Email: test@example.com, IP: 192.168.1.1'")
}

func readStdin() (string, error) {
	var input string
	buf := make([]byte, 4096)
	for {
		n, err := os.Stdin.Read(buf)
		if n > 0 {
			input += string(buf[:n])
		}
		if err != nil {
			break
		}
	}
	return strings.TrimRight(input, "\n"), nil
}

func truncateStr(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}

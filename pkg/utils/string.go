package utils

import "fmt"

// Truncate truncates a string to maxLen characters, appending "..." if truncated.
func Truncate(s string, maxLen int) string {
	if maxLen <= 0 || len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// TruncateDetailed truncates a string showing total character count.
func TruncateDetailed(s string, maxLen int) string {
	if maxLen <= 0 || len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + fmt.Sprintf("... [truncated, total %d chars]", len(s))
}

package gateway

import (
	"fmt"
	"os"
	"path/filepath"
)

// saveMedia saves media data to disk and returns the file path
func saveMedia(data []byte, msgID, platform, mediaType, ext string) string {
	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, ".magic", platform, "media", mediaType)
	os.MkdirAll(dir, 0755)

	filename := fmt.Sprintf("%s_%s.%s", msgID, mediaType, ext)
	path := filepath.Join(dir, filename)
	os.WriteFile(path, data, 0644)
	return path
}

// getMediaDir returns the media directory for a platform
func getMediaDir(platform, mediaType string) string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".magic", platform, "media", mediaType)
}

// ensureMediaDir ensures the media directory exists
func ensureMediaDir(platform, mediaType string) error {
	dir := getMediaDir(platform, mediaType)
	return os.MkdirAll(dir, 0755)
}

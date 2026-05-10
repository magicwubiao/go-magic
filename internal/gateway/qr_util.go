package gateway

import (
	"fmt"
	"os"
	"path/filepath"

	qrcode "github.com/skip2/go-qrcode"
)

// WriteQRPNG writes a QR code PNG file and returns the file path.
// dataDir is used to determine where to save the file.
func WriteQRPNG(data, dataDir string) (string, error) {
	if dataDir == "" {
		home, _ := os.UserHomeDir()
		dataDir = filepath.Join(home, ".magic")
	}
	qrPath := filepath.Join(dataDir, "qr_login.png")
	if err := qrcode.WriteFile(data, qrcode.Medium, 512, qrPath); err != nil {
		return "", fmt.Errorf("failed to generate QR PNG: %w", err)
	}
	return qrPath, nil
}

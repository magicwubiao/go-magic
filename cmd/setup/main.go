package main

import (
	"fmt"
	"os"
	"path/filepath"
)

func main() {
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}

	magicDir := filepath.Join(home, ".magic")
	os.MkdirAll(magicDir, 0755)

	configPath := filepath.Join(magicDir, "config.json")

	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		fmt.Println("Creating default config at", configPath)
		defaultConfig := `{
  "profile": "default",
  "magic_home": "~/.magic",
  "provider": "openai",
  "model": "gpt-4",
  "providers": {
    "openai": {
      "api_key": "your-api-key-here",
      "base_url": "https://api.openai.com/v1",
      "model": "gpt-4"
    }
  }
}`
		os.WriteFile(configPath, []byte(defaultConfig), 0644)
	}

	fmt.Println("go-magic setup complete!")
	fmt.Println("Please edit", configPath, "to add your API key")
}

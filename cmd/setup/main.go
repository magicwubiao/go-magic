package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/magicwubiao/go-magic/pkg/config"
)

func main() {
	magicDir := config.GetMagicHome()
	os.MkdirAll(magicDir, 0755)

	configPath := filepath.Join(magicDir, "config.json")

	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		fmt.Println("Creating default config at", configPath)
		defaultConfig := `{
  "profile": "default",
  "magic_home": "` + magicDir + `",
  "provider": "openai",
  "model": "gpt-5.6",
  "providers": {
    "openai": {
      "api_key": "your-api-key-here",
      "base_url": "https://api.openai.com/v1",
      "model": "gpt-5.6"
    }
  }
}`
		os.WriteFile(configPath, []byte(defaultConfig), 0644)
	}

	fmt.Println("go-magic setup complete!")
	fmt.Println("Please edit", configPath, "to add your API key")
}

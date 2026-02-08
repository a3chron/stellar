package init

import (
	"fmt"
	"os"
	"path/filepath"
)

// EnsureStellarDir creates the ~/.config/stellar directory structure if it doesn't exist
func EnsureStellarDir() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}

	stellarDir := filepath.Join(home, ".config", "stellar")

	// Create main stellar directory
	if err := os.MkdirAll(stellarDir, 0755); err != nil {
		return fmt.Errorf("failed to create stellar directory: %w", err)
	}

	// Create config.json if it doesn't exist
	configPath := filepath.Join(stellarDir, "config.json")
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		// Create empty config
		emptyConfig := []byte(`{
  "current_theme": "",
  "current_path": "",
  "previous_theme": "",
  "previous_path": ""
}`)
		if err := os.WriteFile(configPath, emptyConfig, 0644); err != nil {
			return fmt.Errorf("failed to create config.json: %w", err)
		}
	}

	return nil
}

// StellarDir returns the path to ~/.config/stellar
func StellarDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "stellar"), nil
}

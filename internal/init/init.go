package init

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/a3chron/stellar/internal/paths"
)

// EnsureStellarDir creates the ~/.config/stellar directory structure if it
// doesn't exist. It reports whether config.json had to be created: that is the
// one moment a clean install can be told apart from an existing one, and the
// telemetry code in cmd needs to know before anything else writes the file.
func EnsureStellarDir() (created bool, err error) {
	stellarDir, err := paths.StellarHome()
	if err != nil {
		return false, fmt.Errorf("failed to get stellar home directory: %w", err)
	}

	// Create main stellar directory
	if err := os.MkdirAll(stellarDir, 0755); err != nil {
		return false, fmt.Errorf("failed to create stellar directory: %w", err)
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
			return false, fmt.Errorf("failed to create config.json: %w", err)
		}
		created = true
	}

	return created, nil
}

// StellarDir returns the path to ~/.config/stellar
func StellarDir() (string, error) {
	return paths.StellarHome()
}

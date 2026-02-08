package symlink

import (
	"log"
	"os"
	"path/filepath"
)

func StarshipConfigPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "starship.toml"), nil
}

func CreateSymlink(target string) error {
	configPath, err := StarshipConfigPath()
	if err != nil {
		return err
	}

	// Remove existing symlink/file
	if err := os.Remove(configPath); err != nil {
		log.Printf("warning: failed to remove %s: %v", configPath, err)
	}

	// Create new symlink
	return os.Symlink(target, configPath)
}

func GetCurrentTarget() (string, error) {
	configPath, err := StarshipConfigPath()
	if err != nil {
		return "", err
	}

	target, err := os.Readlink(configPath)
	if err != nil {
		return "", err
	}

	return target, nil
}

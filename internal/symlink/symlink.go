package symlink

import (
	"fmt"
	"io"
	"log"
	"os"
	"os/user"
	"path/filepath"
)

func StarshipConfigPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "starship.toml"), nil
}

// isSymlink checks if the given path is a symlink
func isSymlink(path string) bool {
	info, err := os.Lstat(path)
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeSymlink != 0
}

// backupOriginalConfig backs up the user's original starship.toml to ~/.config/stellar/<username>/backup/latest.toml
// Returns the backup path if successful, empty string otherwise
func backupOriginalConfig(configPath string) (backupPath string, err error) {
	// Check if the file exists and is NOT a symlink
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		return "", nil // No file to back up
	}

	if isSymlink(configPath) {
		return "", nil // Already a symlink, no need to back up
	}

	// Get the current user's username
	currentUser, err := user.Current()
	if err != nil {
		return "", fmt.Errorf("failed to get current user: %w", err)
	}

	// Construct backup path: ~/.config/stellar/<username>/backup/latest.toml
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}

	backupDir := filepath.Join(home, ".config", "stellar", currentUser.Username, "backup")
	backupPath = filepath.Join(backupDir, "latest.toml")

	// Create backup directory
	if err := os.MkdirAll(backupDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create backup directory: %w", err)
	}

	// Copy the original file to backup location
	source, err := os.Open(configPath)
	if err != nil {
		return "", fmt.Errorf("failed to open original config: %w", err)
	}
	defer func() {
		if cerr := source.Close(); cerr != nil && err == nil {
			err = fmt.Errorf("failed to close source file: %w", cerr)
		}
	}()

	destination, err := os.Create(backupPath)
	if err != nil {
		return "", fmt.Errorf("failed to create backup file: %w", err)
	}
	defer func() {
		if cerr := destination.Close(); cerr != nil && err == nil {
			err = fmt.Errorf("failed to close destination file: %w", cerr)
		}
	}()

	if _, err := io.Copy(destination, source); err != nil {
		return "", fmt.Errorf("failed to copy config to backup: %w", err)
	}

	return backupPath, nil
}

// CreateSymlink creates a symlink from ~/.config/starship.toml to the target file.
// If an original (non-symlink) starship.toml exists, it's backed up first.
// Returns the backup path if a backup was created (empty string if no backup was needed).
func CreateSymlink(target string) (backupPath string, err error) {
	configPath, err := StarshipConfigPath()
	if err != nil {
		return "", err
	}

	// Back up original config if it exists and is not a symlink
	backupPath, err = backupOriginalConfig(configPath)
	if err != nil {
		return "", fmt.Errorf("failed to backup original config: %w", err)
	}

	// Remove existing symlink/file
	if err := os.Remove(configPath); err != nil && !os.IsNotExist(err) {
		log.Printf("warning: failed to remove %s: %v", configPath, err)
	}

	// Create new symlink
	if err := os.Symlink(target, configPath); err != nil {
		return backupPath, err
	}

	return backupPath, nil
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

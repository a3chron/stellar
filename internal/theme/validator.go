package theme

import (
	"fmt"
	"os"

	"github.com/BurntSushi/toml"
)

// ValidationResult contains the validation outcome
type ValidationResult struct {
	Valid             bool
	HasCustomCommands bool
	Error             error
}

// ValidateConfig checks if the TOML is valid and identifies security concerns
func ValidateConfig(path string) (ValidationResult, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return ValidationResult{}, err
	}

	return ValidateConfigContent(string(data))
}

// ValidateConfigContent validates TOML content and returns validation result
// Custom commands are detected but NOT blocked - caller decides how to handle
func ValidateConfigContent(content string) (ValidationResult, error) {
	result := ValidationResult{Valid: true}

	// 1. Check TOML syntax
	var config map[string]interface{}
	if _, err := toml.Decode(content, &config); err != nil {
		return ValidationResult{Valid: false, Error: fmt.Errorf("invalid TOML: %w", err)}, nil
	}

	// 2. Check for custom commands (security warning, not blocking)
	if custom, ok := config["custom"]; ok {
		customMap, ok := custom.(map[string]interface{})
		if ok && len(customMap) > 0 {
			result.HasCustomCommands = true
		}
	}

	// 3. Size check (prevent abuse)
	if len(content) > 100*1024 { // 100KB max
		return ValidationResult{Valid: false, Error: fmt.Errorf("config too large (max 100KB)")}, nil
	}

	return result, nil
}

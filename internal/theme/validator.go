package theme

import (
	"fmt"
	"os"

	"github.com/BurntSushi/toml"
)

// ValidateConfig checks if the TOML is valid and safe
func ValidateConfig(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	return ValidateConfigContent(string(data))
}

func ValidateConfigContent(content string) error {
	// 1. Check TOML syntax
	var config map[string]interface{}
	if _, err := toml.Decode(content, &config); err != nil {
		return fmt.Errorf("invalid TOML: %w", err)
	}

	// 2. Check for custom commands (security risk) TODO: just display a warning in this case, ask to proced, default *No*
	if custom, ok := config["custom"]; ok {
		customMap, ok := custom.(map[string]interface{})
		if ok && len(customMap) > 0 {
			return fmt.Errorf("config contains [custom] commands which may execute arbitrary code")
		}
	}

	// 3. Size check (prevent abuse)
	if len(content) > 100*1024 { // 100KB max
		return fmt.Errorf("config too large (max 100KB)")
	}

	return nil
}

package theme

import (
	"testing"

	"github.com/a3chron/stellar/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateConfigContent(t *testing.T) {
	tests := []struct {
		name              string
		content           string
		wantValid         bool
		wantHasCustom     bool
		wantErrorContains string
	}{
		{
			name:          "valid simple config",
			content:       testutil.SampleTOML(),
			wantValid:     true,
			wantHasCustom: false,
		},
		{
			name:          "valid config with custom commands",
			content:       testutil.SampleTOMLWithCustom(),
			wantValid:     true,
			wantHasCustom: true,
		},
		{
			name:              "invalid TOML syntax",
			content:           testutil.InvalidTOML(),
			wantValid:         false,
			wantErrorContains: "invalid TOML",
		},
		{
			name:              "config too large",
			content:           testutil.LargeTOML(),
			wantValid:         false,
			wantErrorContains: "config too large",
		},
		{
			name:          "empty config",
			content:       "",
			wantValid:     true,
			wantHasCustom: false,
		},
		{
			name: "config with empty custom section",
			content: `[character]
success_symbol = ">"

[custom]
`,
			wantValid:     true,
			wantHasCustom: false, // Empty custom section doesn't trigger warning
		},
		{
			name: "config with multiple custom commands",
			content: `[custom.git]
command = "git status"
when = "true"

[custom.date]
command = "date"
when = "true"
`,
			wantValid:     true,
			wantHasCustom: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ValidateConfigContent(tt.content)
			require.NoError(t, err, "ValidateConfigContent should not return error for validation results")

			assert.Equal(t, tt.wantValid, result.Valid)
			assert.Equal(t, tt.wantHasCustom, result.HasCustomCommands)

			if !tt.wantValid && tt.wantErrorContains != "" {
				require.NotNil(t, result.Error)
				assert.Contains(t, result.Error.Error(), tt.wantErrorContains)
			}
		})
	}
}

func TestValidateConfig(t *testing.T) {
	env := testutil.SetupTestEnv(t)

	t.Run("valid file", func(t *testing.T) {
		path := env.CreateThemeFile("test", "valid", "1.0", testutil.SampleTOML())

		result, err := ValidateConfig(path)
		require.NoError(t, err)
		assert.True(t, result.Valid)
		assert.False(t, result.HasCustomCommands)
	})

	t.Run("file with custom commands", func(t *testing.T) {
		path := env.CreateThemeFile("test", "custom", "1.0", testutil.SampleTOMLWithCustom())

		result, err := ValidateConfig(path)
		require.NoError(t, err)
		assert.True(t, result.Valid)
		assert.True(t, result.HasCustomCommands)
	})

	t.Run("invalid file", func(t *testing.T) {
		path := env.CreateThemeFile("test", "invalid", "1.0", testutil.InvalidTOML())

		result, err := ValidateConfig(path)
		require.NoError(t, err)
		assert.False(t, result.Valid)
		assert.Contains(t, result.Error.Error(), "invalid TOML")
	})

	t.Run("nonexistent file", func(t *testing.T) {
		_, err := ValidateConfig(env.StellarDir + "/nonexistent.toml")
		assert.Error(t, err)
	})
}

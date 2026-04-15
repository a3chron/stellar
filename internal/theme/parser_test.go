package theme

import (
	"os"
	"testing"

	"github.com/a3chron/stellar/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseIdentifier(t *testing.T) {
	tests := []struct {
		name        string
		identifier  string
		wantAuthor  string
		wantName    string
		wantVersion string
		wantExplicit bool
		wantErr     bool
	}{
		{
			name:        "simple identifier",
			identifier:  "alice/rainbow",
			wantAuthor:  "alice",
			wantName:    "rainbow",
			wantVersion: "latest",
			wantExplicit: false,
			wantErr:     false,
		},
		{
			name:        "with version",
			identifier:  "alice/rainbow@1.2",
			wantAuthor:  "alice",
			wantName:    "rainbow",
			wantVersion: "1.2",
			wantExplicit: true,
			wantErr:     false,
		},
		{
			name:        "with v prefix version",
			identifier:  "alice/rainbow@v1.2",
			wantAuthor:  "alice",
			wantName:    "rainbow",
			wantVersion: "1.2",
			wantExplicit: true,
			wantErr:     false,
		},
		{
			name:        "with latest version",
			identifier:  "alice/rainbow@latest",
			wantAuthor:  "alice",
			wantName:    "rainbow",
			wantVersion: "latest",
			wantExplicit: true,
			wantErr:     false,
		},
		{
			name:        "with underscores",
			identifier:  "some_user/my_theme",
			wantAuthor:  "some_user",
			wantName:    "my_theme",
			wantVersion: "latest",
			wantExplicit: false,
			wantErr:     false,
		},
		{
			name:        "with hyphens",
			identifier:  "some-user/my-theme",
			wantAuthor:  "some-user",
			wantName:    "my-theme",
			wantVersion: "latest",
			wantExplicit: false,
			wantErr:     false,
		},
		{
			name:        "with numbers",
			identifier:  "user123/theme456",
			wantAuthor:  "user123",
			wantName:    "theme456",
			wantVersion: "latest",
			wantExplicit: false,
			wantErr:     false,
		},
		{
			name:        "with whitespace",
			identifier:  "  alice/rainbow  ",
			wantAuthor:  "alice",
			wantName:    "rainbow",
			wantVersion: "latest",
			wantExplicit: false,
			wantErr:     false,
		},
		{
			name:       "invalid - no slash",
			identifier: "alicerainbow",
			wantErr:    true,
		},
		{
			name:       "invalid - empty author",
			identifier: "/rainbow",
			wantErr:    true,
		},
		{
			name:       "invalid - empty name",
			identifier: "alice/",
			wantErr:    true,
		},
		{
			name:       "invalid - special characters",
			identifier: "alice/rain@bow!",
			wantErr:    true,
		},
		{
			name:       "invalid - triple part version",
			identifier: "alice/rainbow@1.2.3",
			wantErr:    true,
		},
		{
			name:       "invalid - empty",
			identifier: "",
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			theme, err := ParseIdentifier(tt.identifier)

			if tt.wantErr {
				assert.Error(t, err)
				assert.Nil(t, theme)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, theme)
			assert.Equal(t, tt.wantAuthor, theme.Author)
			assert.Equal(t, tt.wantName, theme.Name)
			assert.Equal(t, tt.wantVersion, theme.Version)
			assert.Equal(t, tt.wantExplicit, theme.VersionExplicit)
		})
	}
}

func TestTheme_String(t *testing.T) {
	tests := []struct {
		name     string
		theme    Theme
		expected string
	}{
		{
			name:     "with latest version",
			theme:    Theme{Author: "alice", Name: "rainbow", Version: "latest"},
			expected: "alice/rainbow",
		},
		{
			name:     "with specific version",
			theme:    Theme{Author: "alice", Name: "rainbow", Version: "1.2"},
			expected: "alice/rainbow@1.2",
		},
		{
			name:     "with zero version",
			theme:    Theme{Author: "bob", Name: "sunset", Version: "0.1"},
			expected: "bob/sunset@0.1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.theme.String())
		})
	}
}

func TestCompareSemver(t *testing.T) {
	tests := []struct {
		name   string
		a      string
		b      string
		expect int // >0 if a > b, <0 if a < b, 0 if equal
	}{
		{
			name:   "equal versions",
			a:      "1.0",
			b:      "1.0",
			expect: 0,
		},
		{
			name:   "a newer major",
			a:      "2.0",
			b:      "1.0",
			expect: 1,
		},
		{
			name:   "b newer major",
			a:      "1.0",
			b:      "2.0",
			expect: -1,
		},
		{
			name:   "a newer minor",
			a:      "1.5",
			b:      "1.2",
			expect: 1,
		},
		{
			name:   "b newer minor",
			a:      "1.2",
			b:      "1.5",
			expect: -1,
		},
		{
			name:   "latest vs semver - latest goes after",
			a:      "latest",
			b:      "1.0",
			expect: -1,
		},
		{
			name:   "semver vs latest - semver goes before",
			a:      "1.0",
			b:      "latest",
			expect: 1,
		},
		{
			name:   "both non-semver",
			a:      "alpha",
			b:      "beta",
			expect: -1, // alphabetical comparison
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := compareSemver(tt.a, tt.b)
			if tt.expect > 0 {
				assert.Greater(t, result, 0, "expected %s > %s", tt.a, tt.b)
			} else if tt.expect < 0 {
				assert.Less(t, result, 0, "expected %s < %s", tt.a, tt.b)
			} else {
				assert.Equal(t, 0, result, "expected %s == %s", tt.a, tt.b)
			}
		})
	}
}

func TestParseSemver(t *testing.T) {
	tests := []struct {
		name      string
		version   string
		wantMajor int
		wantMinor int
		wantOk    bool
	}{
		{
			name:      "valid 1.0",
			version:   "1.0",
			wantMajor: 1,
			wantMinor: 0,
			wantOk:    true,
		},
		{
			name:      "valid 2.5",
			version:   "2.5",
			wantMajor: 2,
			wantMinor: 5,
			wantOk:    true,
		},
		{
			name:      "valid 10.20",
			version:   "10.20",
			wantMajor: 10,
			wantMinor: 20,
			wantOk:    true,
		},
		{
			name:    "invalid - no dot",
			version: "10",
			wantOk:  false,
		},
		{
			name:    "invalid - triple",
			version: "1.2.3",
			wantOk:  false,
		},
		{
			name:    "invalid - latest",
			version: "latest",
			wantOk:  false,
		},
		{
			name:    "invalid - text",
			version: "abc.def",
			wantOk:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			major, minor, ok := parseSemver(tt.version)
			assert.Equal(t, tt.wantOk, ok)
			if tt.wantOk {
				assert.Equal(t, tt.wantMajor, major)
				assert.Equal(t, tt.wantMinor, minor)
			}
		})
	}
}

func TestTheme_CachePath(t *testing.T) {
	// Use test environment to isolate from real filesystem
	env := testutil.SetupTestEnv(t)

	theme := &Theme{
		Author:  "alice",
		Name:    "rainbow",
		Version: "1.2",
	}

	path, err := theme.CachePath()
	require.NoError(t, err)

	// Should be relative to the test stellar home
	expected := env.StellarDir + "/alice/rainbow/1.2.toml"
	assert.Equal(t, expected, path)
}

func TestTheme_CacheDir(t *testing.T) {
	env := testutil.SetupTestEnv(t)

	theme := &Theme{
		Author:  "alice",
		Name:    "rainbow",
		Version: "1.2",
	}

	dir, err := theme.CacheDir()
	require.NoError(t, err)

	expected := env.StellarDir + "/alice/rainbow"
	assert.Equal(t, expected, dir)
}

func TestFindLatestLocalVersion(t *testing.T) {
	env := testutil.SetupTestEnv(t)

	// Create theme files
	env.CreateThemeFile("alice", "rainbow", "1.0", testutil.SampleTOML())
	env.CreateThemeFile("alice", "rainbow", "1.5", testutil.SampleTOML())
	env.CreateThemeFile("alice", "rainbow", "2.0", testutil.SampleTOML())

	theme := &Theme{Author: "alice", Name: "rainbow", Version: "latest"}
	themeDir, err := theme.CacheDir()
	require.NoError(t, err)

	version, err := FindLatestLocalVersion(themeDir)
	require.NoError(t, err)
	assert.Equal(t, "2.0", version)
}

func TestFindLatestLocalVersion_WithLatestFile(t *testing.T) {
	env := testutil.SetupTestEnv(t)

	// Create versioned files and a "latest" file
	env.CreateThemeFile("alice", "rainbow", "1.0", testutil.SampleTOML())
	env.CreateThemeFile("alice", "rainbow", "latest", testutil.SampleTOML())

	theme := &Theme{Author: "alice", Name: "rainbow", Version: "latest"}
	themeDir, err := theme.CacheDir()
	require.NoError(t, err)

	version, err := FindLatestLocalVersion(themeDir)
	require.NoError(t, err)
	// Semver should come before "latest"
	assert.Equal(t, "1.0", version)
}

func TestFindLatestLocalVersion_Empty(t *testing.T) {
	env := testutil.SetupTestEnv(t)

	// Create empty theme directory (no .toml files)
	themeDir := env.StellarDir + "/alice/empty"
	require.NoError(t, createDir(themeDir))

	_, err := FindLatestLocalVersion(themeDir)
	assert.Error(t, err)
}

func TestFindLatestLocalVersion_NonexistentDir(t *testing.T) {
	env := testutil.SetupTestEnv(t)

	_, err := FindLatestLocalVersion(env.StellarDir + "/nonexistent/theme")
	assert.Error(t, err)
}

// Helper to create directory
func createDir(path string) error {
	return os.MkdirAll(path, 0755)
}

// Package testutil provides test environment setup and helpers for stellar-cli tests.
// It enables complete test isolation by redirecting all paths to temporary directories.
package testutil

import (
	"bytes"
	"io"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/a3chron/stellar/internal/paths"
	"github.com/fatih/color"
)

// TestEnv holds the test environment configuration
type TestEnv struct {
	t            *testing.T
	RootDir      string // Root temp directory
	StellarDir   string // ~/.config/stellar equivalent
	StarshipPath string // ~/.config/starship.toml equivalent
	TmpDir       string // /tmp/stellar equivalent

	// Original env values for restoration
	origEnv map[string]string

	// Mock API server (if set up)
	MockServer *httptest.Server
}

// SetupTestEnv creates an isolated test environment with temporary directories
// and sets environment variables to redirect all paths.
// Cleanup is automatic via t.Cleanup().
func SetupTestEnv(t *testing.T) *TestEnv {
	t.Helper()

	rootDir := t.TempDir() // Auto-cleaned by Go

	env := &TestEnv{
		t:            t,
		RootDir:      rootDir,
		StellarDir:   filepath.Join(rootDir, "stellar"),
		StarshipPath: filepath.Join(rootDir, "starship.toml"),
		TmpDir:       filepath.Join(rootDir, "tmp"),
		origEnv:      make(map[string]string),
	}

	// Create directories
	if err := os.MkdirAll(env.StellarDir, 0755); err != nil {
		t.Fatalf("failed to create stellar dir: %v", err)
	}
	if err := os.MkdirAll(env.TmpDir, 0755); err != nil {
		t.Fatalf("failed to create tmp dir: %v", err)
	}

	// Save original env values
	env.origEnv[paths.EnvStellarHome] = os.Getenv(paths.EnvStellarHome)
	env.origEnv[paths.EnvStarshipPath] = os.Getenv(paths.EnvStarshipPath)
	env.origEnv[paths.EnvTmpDir] = os.Getenv(paths.EnvTmpDir)
	env.origEnv[paths.EnvAPIURL] = os.Getenv(paths.EnvAPIURL)
	env.origEnv[paths.EnvApplyMode] = os.Getenv(paths.EnvApplyMode)
	env.origEnv[paths.EnvNoTelemetry] = os.Getenv(paths.EnvNoTelemetry)
	env.origEnv[paths.EnvDoNotTrack] = os.Getenv(paths.EnvDoNotTrack)
	// STARSHIP_CONFIG isn't a stellar-owned env var, but apply/rollback now
	// warn when it points somewhere other than the file stellar manages (see
	// cmd.warnPostApplyEnvironment). A developer's own shell commonly has it
	// set (to their real starship.toml), which would otherwise leak into
	// every test's output and make that warning's presence/absence
	// environment-dependent instead of deterministic.
	env.origEnv["STARSHIP_CONFIG"] = os.Getenv("STARSHIP_CONFIG")
	_ = os.Unsetenv("STARSHIP_CONFIG")

	// TERMINAL/DISPLAY/WAYLAND_DISPLAY affect `stellar preview`'s terminal
	// selection (see cmd/preview.go's previewTerminalCandidates). Clearing
	// them keeps that selection - and any assertions about it - independent
	// of whatever desktop session happens to be running the tests, instead
	// of depending on the developer's own $TERMINAL or a real X/Wayland
	// session being present. This is belt-and-suspenders alongside
	// cmd.startTerminal's testing.Testing() guard, which is what actually
	// stops a test from ever spawning a real terminal window - this just
	// keeps output deterministic.
	for _, key := range []string{"TERMINAL", "DISPLAY", "WAYLAND_DISPLAY"} {
		env.origEnv[key] = os.Getenv(key)
		_ = os.Unsetenv(key)
	}

	// Telemetry is off by default in tests. Any test that pins a non-dev
	// version (the update tests do) would otherwise POST a real install
	// report to production stellar-hub - and wait up to 2s for it. Tests of
	// the telemetry itself call EnableTelemetry() after SetupTestEnv.
	_ = os.Setenv(paths.EnvNoTelemetry, "1")
	_ = os.Unsetenv(paths.EnvDoNotTrack)

	// Set test env variables
	_ = os.Setenv(paths.EnvStellarHome, env.StellarDir)
	_ = os.Setenv(paths.EnvStarshipPath, env.StarshipPath)
	_ = os.Setenv(paths.EnvTmpDir, env.TmpDir)
	// Pin apply mode to symlink so the suite is deterministic regardless of
	// the OS running it (IsCopyMode defaults to copy on Windows). Tests that
	// specifically want copy-mode behavior call t.Setenv(paths.EnvApplyMode,
	// "copy") themselves *after* calling SetupTestEnv, so their override wins.
	_ = os.Setenv(paths.EnvApplyMode, "symlink")

	// Register cleanup
	t.Cleanup(func() {
		env.cleanup()
	})

	return env
}

// cleanup restores original environment variables
func (e *TestEnv) cleanup() {
	// Close mock server if running
	if e.MockServer != nil {
		e.MockServer.Close()
	}

	// Restore original env values
	for key, value := range e.origEnv {
		if value == "" {
			_ = os.Unsetenv(key)
		} else {
			_ = os.Setenv(key, value)
		}
	}
}

// EnableTelemetry lifts the default opt-out set by SetupTestEnv (and clears a
// developer's own DO_NOT_TRACK) so a test can observe install reports against
// the mock API. Always pair it with SetupMockAPI - never let a test report to
// production. The original values are restored by the usual cleanup.
func (e *TestEnv) EnableTelemetry() {
	_ = os.Unsetenv(paths.EnvNoTelemetry)
	_ = os.Unsetenv(paths.EnvDoNotTrack)
}

// SetupMockAPI starts a mock API server and sets STELLAR_API_URL
func (e *TestEnv) SetupMockAPI(handler *MockAPIHandler) {
	e.MockServer = httptest.NewServer(handler)
	_ = os.Setenv(paths.EnvAPIURL, e.MockServer.URL)
}

// CreateThemeFile creates a theme config file in the test cache
func (e *TestEnv) CreateThemeFile(author, name, version, content string) string {
	themeDir := filepath.Join(e.StellarDir, author, name)
	if err := os.MkdirAll(themeDir, 0755); err != nil {
		e.t.Fatalf("failed to create theme dir: %v", err)
	}

	themePath := filepath.Join(themeDir, version+".toml")
	if err := os.WriteFile(themePath, []byte(content), 0644); err != nil {
		e.t.Fatalf("failed to write theme file: %v", err)
	}

	return themePath
}

// CreateConfig creates a config.json file in the test stellar directory.
//
// Callers build the JSON by concatenating filesystem paths into it, which is
// invalid JSON on Windows: a path like C:\Users\... contains \U, and encoding/
// json rejects it as a bad escape ("invalid character 'U' in string escape
// code"). Backslashes are therefore escaped here rather than at all ~30 call
// sites. No test wants a real JSON escape sequence in this content, so this
// is safe - but if one ever does, it needs to pass \\ itself.
func (e *TestEnv) CreateConfig(content string) string {
	configPath := filepath.Join(e.StellarDir, "config.json")
	content = strings.ReplaceAll(content, `\`, `\\`)
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		e.t.Fatalf("failed to write config file: %v", err)
	}
	return configPath
}

// CreateStarshipConfig creates a starship.toml file (non-symlink)
func (e *TestEnv) CreateStarshipConfig(content string) {
	if err := os.WriteFile(e.StarshipPath, []byte(content), 0644); err != nil {
		e.t.Fatalf("failed to write starship config: %v", err)
	}
}

// CreateTmpThemeFile creates a theme file in the tmp cache (for preview)
func (e *TestEnv) CreateTmpThemeFile(author, name, version, content string) string {
	themeDir := filepath.Join(e.TmpDir, author, name)
	if err := os.MkdirAll(themeDir, 0755); err != nil {
		e.t.Fatalf("failed to create tmp theme dir: %v", err)
	}

	themePath := filepath.Join(themeDir, version+".toml")
	if err := os.WriteFile(themePath, []byte(content), 0644); err != nil {
		e.t.Fatalf("failed to write tmp theme file: %v", err)
	}

	return themePath
}

// ReadFile reads a file and returns its contents, failing the test on error
func (e *TestEnv) ReadFile(path string) string {
	content, err := os.ReadFile(path)
	if err != nil {
		e.t.Fatalf("failed to read file %s: %v", path, err)
	}
	return string(content)
}

// FileExists checks if a file exists
func (e *TestEnv) FileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// IsSymlink checks if a path is a symlink
func (e *TestEnv) IsSymlink(path string) bool {
	info, err := os.Lstat(path)
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeSymlink != 0
}

// ReadSymlink returns the target of a symlink
func (e *TestEnv) ReadSymlink(path string) string {
	target, err := os.Readlink(path)
	if err != nil {
		e.t.Fatalf("failed to read symlink %s: %v", path, err)
	}
	return target
}

// CaptureOutput runs fn with stdout redirected to a pipe and returns everything
// written to it. It swaps both os.Stdout and color.Output: fatih/color captures
// os.Stdout at package-init time, so reassigning os.Stdout alone would miss any
// color.* output. Both are restored before returning. The pipe is drained in a
// goroutine so large output can't deadlock on the pipe buffer.
//
// The restore is deferred and idempotent: if fn panics (e.g. a t.Fatal inside
// it), os.Stdout/color.Output are still put back before the panic propagates,
// instead of staying pointed at a pipe that no longer has a reader for every
// later test in the process.
func CaptureOutput(t *testing.T, fn func()) string {
	t.Helper()

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("failed to create pipe: %v", err)
	}

	origStdout := os.Stdout
	origColorOutput := color.Output
	os.Stdout = w
	color.Output = w

	done := make(chan string, 1)
	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)
		done <- buf.String()
	}()

	restored := false
	restore := func() {
		if !restored {
			restored = true
			os.Stdout = origStdout
			color.Output = origColorOutput
			_ = w.Close()
		}
	}
	defer restore()

	fn()

	// Restore first, then close the writer so the reader goroutine sees EOF.
	restore()

	out := <-done
	if cerr := r.Close(); cerr != nil {
		t.Fatalf("failed to close pipe reader: %v", cerr)
	}
	return out
}

// RequireSymlinks skips the test if the environment can't create symlinks
// (e.g. Windows without Developer Mode or admin privileges). Call it at the
// top of any test that exercises symlink-mode behavior directly.
func RequireSymlinks(t *testing.T) {
	t.Helper()

	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	link := filepath.Join(dir, "link")

	if err := os.WriteFile(target, []byte("probe"), 0644); err != nil {
		t.Fatalf("failed to create symlink probe target: %v", err)
	}

	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks not supported in this environment: %v", err)
	}
}

// SampleTOML returns a valid sample starship config
func SampleTOML() string {
	return `# Sample starship config
format = "$all"

[character]
success_symbol = "[>](bold green)"
error_symbol = "[x](bold red)"
`
}

// SampleTOMLWithCustom returns a sample config with custom commands
func SampleTOMLWithCustom() string {
	return `# Sample starship config with custom commands
format = "$all"

[character]
success_symbol = "[>](bold green)"

[custom.example]
command = "echo hello"
when = "true"
`
}

// InvalidTOML returns an invalid TOML string
func InvalidTOML() string {
	return `this is not valid toml [
format = "missing quote
`
}

// LargeTOML returns a TOML string larger than 100KB
func LargeTOML() string {
	// Generate a TOML that exceeds 100KB
	content := "[character]\n"
	// Add padding to exceed 100KB
	for i := 0; i < 10000; i++ {
		content += "# padding line to make this file very large\n"
	}
	return content
}

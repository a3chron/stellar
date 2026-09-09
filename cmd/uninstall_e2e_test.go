// E2E tests for "stellar uninstall". They go through NewRootCmd().Execute(),
// with executablePath pointed at a scratch file so the command never deletes
// the go-test binary it is running inside.
package cmd

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/a3chron/stellar/internal/telemetry"
	"github.com/a3chron/stellar/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeBinary writes a scratch file and points executablePath at it, so the
// uninstall command deletes that instead of the running go-test binary.
func fakeBinary(t *testing.T, env *testutil.TestEnv) string {
	t.Helper()
	path := filepath.Join(env.RootDir, "bin", "stellar")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0755))
	require.NoError(t, os.WriteFile(path, []byte("fake stellar binary"), 0755))

	orig := executablePath
	executablePath = func() (string, error) { return path, nil }
	t.Cleanup(func() { executablePath = orig })
	return path
}

// runUninstall executes "stellar uninstall" with stdin replaced by in (nil
// leaves cobra's default, the process stdin) and stdout captured.
func runUninstall(t *testing.T, in io.Reader, args ...string) (string, error) {
	t.Helper()
	var execErr error
	out := testutil.CaptureOutput(t, func() {
		cmd := NewRootCmd()
		cmd.SetArgs(append([]string{"uninstall"}, args...))
		cmd.SetOut(new(bytes.Buffer))
		if in != nil {
			cmd.SetIn(in)
		}
		execErr = cmd.Execute()
	})
	return out, execErr
}

// pipeStdin returns the read end of a closed pipe: an *os.File that is not a
// character device, which is what stdin looks like under `echo y | stellar
// uninstall` or in CI.
func pipeStdin(t *testing.T) *os.File {
	t.Helper()
	r, w, err := os.Pipe()
	require.NoError(t, err)
	require.NoError(t, w.Close())
	t.Cleanup(func() { _ = r.Close() })
	return r
}

func TestE2E_Uninstall(t *testing.T) {
	t.Run("Refuses without --yes when stdin is not a terminal", func(t *testing.T) {
		env := testutil.SetupTestEnv(t)
		resetFlags()
		binary := fakeBinary(t, env)

		_, err := runUninstall(t, pipeStdin(t))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "--yes")
		assert.True(t, env.FileExists(binary), "nothing may be removed before confirmation")
		assert.True(t, env.FileExists(env.StellarDir))
	})

	t.Run("Answering no aborts without changes", func(t *testing.T) {
		env := testutil.SetupTestEnv(t)
		resetFlags()
		binary := fakeBinary(t, env)

		out, err := runUninstall(t, strings.NewReader("n\n"))
		require.NoError(t, err)
		assert.Contains(t, out, "Aborted")
		assert.True(t, env.FileExists(binary))
		assert.True(t, env.FileExists(env.StellarDir))
	})

	t.Run("Removes binary and stellar home, detaches the prompt, tells the hub", func(t *testing.T) {
		testutil.RequireSymlinks(t)
		env := testutil.SetupTestEnv(t)
		resetFlags()
		pinVersion(t, "1.2.3")
		env.EnableTelemetry()
		mockAPI := testutil.CreateDefaultMockAPI()
		env.SetupMockAPI(mockAPI)
		binary := fakeBinary(t, env)

		themePath := env.CreateThemeFile("local", "mytheme", "1.0", testutil.SampleTOML())
		require.NoError(t, os.Symlink(themePath, env.StarshipPath))
		env.CreateConfig(`{
  "current_theme": "local/mytheme@1.0",
  "current_path": "` + themePath + `",
  "install_id": "11111111-2222-4333-8444-555555555555",
  "install_kind": "install",
  "reported_version": "1.2.3",
  "reported_at": "2024-01-01T00:00:00Z"
}`)

		out, err := runUninstall(t, nil, "--yes")
		require.NoError(t, err)
		assert.Contains(t, out, "uninstalled")

		// The hub got exactly one message: the uninstall, under the same id.
		pings := mockAPI.Pings()
		require.Len(t, pings, 1)
		assert.Equal(t, telemetry.EventUninstall, pings[0].Event)
		assert.Equal(t, "11111111-2222-4333-8444-555555555555", pings[0].ID)
		assert.Equal(t, "1.2.3", pings[0].Version)
		assert.Equal(t, "1.2.3", pings[0].Previous)

		// The prompt survives as a regular file with the theme's content.
		assert.False(t, env.IsSymlink(env.StarshipPath))
		assert.Equal(t, testutil.SampleTOML(), env.ReadFile(env.StarshipPath))

		assert.False(t, env.FileExists(binary))
		assert.False(t, env.FileExists(env.StellarDir), "stellar home must be gone, and not re-created by telemetry")
	})

	t.Run("--keep-config keeps the stellar directory", func(t *testing.T) {
		env := testutil.SetupTestEnv(t)
		resetFlags()
		binary := fakeBinary(t, env)
		env.CreateConfig(`{"current_theme": "", "current_path": ""}`)

		_, err := runUninstall(t, nil, "--yes", "--keep-config")
		require.NoError(t, err)

		assert.False(t, env.FileExists(binary))
		assert.True(t, env.FileExists(filepath.Join(env.StellarDir, "config.json")))
	})

	t.Run("Regular starship.toml is left alone", func(t *testing.T) {
		env := testutil.SetupTestEnv(t)
		resetFlags()
		fakeBinary(t, env)
		env.CreateStarshipConfig("# my own config\n")

		_, err := runUninstall(t, nil, "--yes")
		require.NoError(t, err)
		assert.Equal(t, "# my own config\n", env.ReadFile(env.StarshipPath))
	})

	t.Run("Symlink to somewhere else is left alone", func(t *testing.T) {
		testutil.RequireSymlinks(t)
		env := testutil.SetupTestEnv(t)
		resetFlags()
		fakeBinary(t, env)
		elsewhere := filepath.Join(env.RootDir, "elsewhere.toml")
		require.NoError(t, os.WriteFile(elsewhere, []byte("# elsewhere\n"), 0644))
		require.NoError(t, os.Symlink(elsewhere, env.StarshipPath))

		_, err := runUninstall(t, nil, "--yes")
		require.NoError(t, err)
		assert.True(t, env.IsSymlink(env.StarshipPath))
		assert.Equal(t, elsewhere, env.ReadSymlink(env.StarshipPath))
	})

	t.Run("Dev build does not tell the hub", func(t *testing.T) {
		env := testutil.SetupTestEnv(t)
		resetFlags()
		env.EnableTelemetry()
		mockAPI := testutil.CreateDefaultMockAPI()
		env.SetupMockAPI(mockAPI)
		fakeBinary(t, env)
		env.CreateConfig(`{"install_id": "11111111-2222-4333-8444-555555555555", "install_kind": "install"}`)

		_, err := runUninstall(t, nil, "--yes")
		require.NoError(t, err)
		assert.Empty(t, mockAPI.Pings())
	})
}

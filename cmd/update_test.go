package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRemoveUpdateLeftovers covers the worker split out of
// cleanupUpdateLeftovers so the removal logic is testable on any OS, even
// though the gate that calls it in production only fires on Windows.
func TestRemoveUpdateLeftovers(t *testing.T) {
	dir := t.TempDir()

	execPath := filepath.Join(dir, "stellar.exe")
	oldBinary := execPath + ".old"
	tmpUpdate := filepath.Join(dir, ".stellar-update-abc123")
	unrelated := filepath.Join(dir, "keep-me.txt")
	unrelatedOld := filepath.Join(dir, "other-tool.old")

	require.NoError(t, os.WriteFile(oldBinary, []byte("old binary"), 0644))
	require.NoError(t, os.WriteFile(tmpUpdate, []byte("partial download"), 0644))
	require.NoError(t, os.WriteFile(unrelated, []byte("unrelated"), 0644))
	require.NoError(t, os.WriteFile(unrelatedOld, []byte("someone else's leftover"), 0644))

	removeUpdateLeftovers(execPath)

	assert.NoFileExists(t, oldBinary, "leftover .old binary should be removed")
	assert.NoFileExists(t, tmpUpdate, "leftover update temp file should be removed")
	assert.FileExists(t, unrelated, "unrelated files must not be touched")
	assert.FileExists(t, unrelatedOld, "another tool's .old file must not be touched")
}

// TestRemoveUpdateLeftovers_NothingToRemove verifies the worker is a safe
// no-op when there's nothing left over from a previous update.
func TestRemoveUpdateLeftovers_NothingToRemove(t *testing.T) {
	dir := t.TempDir()

	require.NotPanics(t, func() {
		removeUpdateLeftovers(filepath.Join(dir, "stellar.exe"))
	})
}

// TestRemoveUpdateLeftovers_BracketDirName proves the fix for the
// filepath.Glob metacharacter bug: Glob interprets "[", "]" etc. in the
// directory path itself as pattern syntax, so an install dir like
// "tools[1]" used to silently disable cleanup (either matching nothing, or
// hitting ErrBadPattern, which was swallowed). os.ReadDir + strings.HasPrefix
// has no such issue since the directory is opened directly rather than
// pattern-matched.
func TestRemoveUpdateLeftovers_BracketDirName(t *testing.T) {
	base := t.TempDir()
	dir := filepath.Join(base, "tools[1]")
	require.NoError(t, os.Mkdir(dir, 0755))

	execPath := filepath.Join(dir, "stellar.exe")
	oldBinary := execPath + ".old"
	tmpUpdate := filepath.Join(dir, ".stellar-update-xyz789")

	require.NoError(t, os.WriteFile(oldBinary, []byte("old binary"), 0644))
	require.NoError(t, os.WriteFile(tmpUpdate, []byte("partial download"), 0644))

	removeUpdateLeftovers(execPath)

	assert.NoFileExists(t, oldBinary, "leftover .old binary should be removed even when the dir name contains glob metacharacters")
	assert.NoFileExists(t, tmpUpdate, "leftover update temp file should be removed even when the dir name contains glob metacharacters")
}

// TestCleanupUpdateLeftovers_RemovesRealArtifacts verifies the
// PersistentPreRunE gate cleans up leftovers next to the actual running
// binary (os.Executable() in a test resolves to the compiled test binary).
// This now runs unconditionally on every OS: temp files are created next to
// the binary regardless of platform, so an interrupted update (Ctrl-C,
// SIGKILL) would litter the bin dir on Linux/macOS too if cleanup were still
// gated to Windows only.
func TestCleanupUpdateLeftovers_RemovesRealArtifacts(t *testing.T) {
	execPath, err := os.Executable()
	require.NoError(t, err)

	oldBinary := execPath + ".old"
	tmpUpdate := filepath.Join(filepath.Dir(execPath), ".stellar-update-test123")

	require.NoError(t, os.WriteFile(oldBinary, []byte("old binary"), 0644))
	require.NoError(t, os.WriteFile(tmpUpdate, []byte("partial download"), 0644))
	t.Cleanup(func() {
		_ = os.Remove(oldBinary)
		_ = os.Remove(tmpUpdate)
	})

	require.NotPanics(t, func() {
		cleanupUpdateLeftovers()
	})

	assert.NoFileExists(t, oldBinary, "leftover .old binary next to the real binary should be removed")
	assert.NoFileExists(t, tmpUpdate, "leftover update temp file next to the real binary should be removed")
}

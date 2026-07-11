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

// TestCleanupUpdateLeftovers_GateIsSafe verifies the PersistentPreRunE gate
// itself never panics or errors when called directly (it early-returns on
// non-Windows platforms, which covers CI).
func TestCleanupUpdateLeftovers_GateIsSafe(t *testing.T) {
	require.NotPanics(t, func() {
		cleanupUpdateLeftovers()
	})
}

// E2E tests for the anonymous install report. They go through
// NewRootCmd().Execute(), so the PersistentPreRunE / PersistentPostRunE wiring
// is exercised exactly as a user's shell would.
package cmd

import (
	"bytes"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/a3chron/stellar/internal/config"
	"github.com/a3chron/stellar/internal/paths"
	"github.com/a3chron/stellar/internal/telemetry"
	"github.com/a3chron/stellar/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var uuidV4Pattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

// runList executes "stellar list" - the cheapest command that goes through the
// full PreRun/RunE/PostRun chain - with stdout captured. Execute() returns only
// after PersistentPostRunE has joined the report, so assertions that follow
// need no sleeps.
func runList(t *testing.T) (string, error) {
	t.Helper()
	var execErr error
	out := testutil.CaptureOutput(t, func() {
		cmd := NewRootCmd()
		cmd.SetArgs([]string{"list"})
		cmd.SetOut(new(bytes.Buffer))
		execErr = cmd.Execute()
	})
	return out, execErr
}

func TestE2E_Telemetry(t *testing.T) {
	t.Run("Dev build sends nothing", func(t *testing.T) {
		env := testutil.SetupTestEnv(t)
		resetFlags()
		env.EnableTelemetry()
		mockAPI := testutil.CreateDefaultMockAPI()
		env.SetupMockAPI(mockAPI)

		_, err := runList(t)
		require.NoError(t, err)

		assert.Empty(t, mockAPI.Pings())
		cfg, err := config.Load()
		require.NoError(t, err)
		assert.Empty(t, cfg.InstallID, "a dev build must not even mint an id")
	})

	t.Run("Fresh install reports kind install and persists its identity", func(t *testing.T) {
		env := testutil.SetupTestEnv(t)
		resetFlags()
		pinVersion(t, "1.2.3")
		env.EnableTelemetry()
		mockAPI := testutil.CreateDefaultMockAPI()
		env.SetupMockAPI(mockAPI)

		out, err := runList(t)
		require.NoError(t, err)
		assert.NotContains(t, out, "ping", "telemetry must be silent")

		pings := mockAPI.Pings()
		require.Len(t, pings, 1)
		assert.Regexp(t, uuidV4Pattern, pings[0].ID)
		assert.Equal(t, telemetry.KindInstall, pings[0].Kind)
		assert.Equal(t, telemetry.EventReport, pings[0].Event)
		assert.Equal(t, "1.2.3", pings[0].Version)
		assert.Equal(t, "", pings[0].Previous)
		assert.Equal(t, runtime.GOOS, pings[0].OS)
		assert.Equal(t, runtime.GOARCH, pings[0].Arch)

		cfg, err := config.Load()
		require.NoError(t, err)
		assert.Equal(t, pings[0].ID, cfg.InstallID)
		assert.Equal(t, telemetry.KindInstall, cfg.InstallKind)
		assert.Equal(t, "1.2.3", cfg.ReportedVersion)
		reportedAt, err := time.Parse(time.RFC3339, cfg.ReportedAt)
		require.NoError(t, err)
		assert.WithinDuration(t, time.Now(), reportedAt, time.Minute)
	})

	t.Run("Pre-existing config without an id reports kind existing", func(t *testing.T) {
		env := testutil.SetupTestEnv(t)
		resetFlags()
		pinVersion(t, "1.2.3")
		env.EnableTelemetry()
		mockAPI := testutil.CreateDefaultMockAPI()
		env.SetupMockAPI(mockAPI)
		env.CreateConfig(`{"current_theme": "", "current_path": ""}`)

		_, err := runList(t)
		require.NoError(t, err)

		pings := mockAPI.Pings()
		require.Len(t, pings, 1)
		assert.Equal(t, telemetry.KindExisting, pings[0].Kind)

		cfg, err := config.Load()
		require.NoError(t, err)
		assert.Equal(t, telemetry.KindExisting, cfg.InstallKind)
	})

	t.Run("Same version again sends nothing", func(t *testing.T) {
		env := testutil.SetupTestEnv(t)
		resetFlags()
		pinVersion(t, "1.2.3")
		env.EnableTelemetry()
		mockAPI := testutil.CreateDefaultMockAPI()
		env.SetupMockAPI(mockAPI)
		env.CreateConfig(`{
  "install_id": "11111111-2222-4333-8444-555555555555",
  "install_kind": "install",
  "reported_version": "1.2.3",
  "reported_at": "2024-01-01T00:00:00Z"
}`)

		_, err := runList(t)
		require.NoError(t, err)
		assert.Empty(t, mockAPI.Pings())

		cfg, err := config.Load()
		require.NoError(t, err)
		assert.Equal(t, "2024-01-01T00:00:00Z", cfg.ReportedAt, "nothing to record when nothing was sent")
	})

	t.Run("Version change reports with previous set", func(t *testing.T) {
		env := testutil.SetupTestEnv(t)
		resetFlags()
		pinVersion(t, "1.2.3")
		env.EnableTelemetry()
		mockAPI := testutil.CreateDefaultMockAPI()
		env.SetupMockAPI(mockAPI)
		env.CreateConfig(`{
  "install_id": "11111111-2222-4333-8444-555555555555",
  "install_kind": "existing",
  "reported_version": "1.2.2",
  "reported_at": "2024-01-01T00:00:00Z"
}`)

		_, err := runList(t)
		require.NoError(t, err)

		pings := mockAPI.Pings()
		require.Len(t, pings, 1)
		assert.Equal(t, "11111111-2222-4333-8444-555555555555", pings[0].ID)
		assert.Equal(t, telemetry.KindExisting, pings[0].Kind, "kind is fixed at first identity, never revisited")
		assert.Equal(t, "1.2.2", pings[0].Previous)
		assert.Equal(t, "1.2.3", pings[0].Version)

		cfg, err := config.Load()
		require.NoError(t, err)
		assert.Equal(t, "1.2.3", cfg.ReportedVersion)
	})

	t.Run("STELLAR_NO_TELEMETRY opts out", func(t *testing.T) {
		env := testutil.SetupTestEnv(t)
		resetFlags()
		pinVersion(t, "1.2.3")
		env.EnableTelemetry()
		t.Setenv(paths.EnvNoTelemetry, "1")
		mockAPI := testutil.CreateDefaultMockAPI()
		env.SetupMockAPI(mockAPI)

		_, err := runList(t)
		require.NoError(t, err)
		assert.Empty(t, mockAPI.Pings())

		cfg, err := config.Load()
		require.NoError(t, err)
		assert.Empty(t, cfg.InstallID, "opting out must not mint an id either")
	})

	t.Run("DO_NOT_TRACK opts out", func(t *testing.T) {
		env := testutil.SetupTestEnv(t)
		resetFlags()
		pinVersion(t, "1.2.3")
		env.EnableTelemetry()
		t.Setenv(paths.EnvDoNotTrack, "1")
		mockAPI := testutil.CreateDefaultMockAPI()
		env.SetupMockAPI(mockAPI)

		_, err := runList(t)
		require.NoError(t, err)
		assert.Empty(t, mockAPI.Pings())
	})

	t.Run("Server error leaves the report pending and the command unaffected", func(t *testing.T) {
		env := testutil.SetupTestEnv(t)
		resetFlags()
		pinVersion(t, "1.2.3")
		env.EnableTelemetry()
		mockAPI := testutil.CreateDefaultMockAPI()
		mockAPI.SetPingStatus(500)
		env.SetupMockAPI(mockAPI)

		out, err := runList(t)
		require.NoError(t, err)
		assert.Contains(t, out, "No themes cached yet")
		assert.NotContains(t, strings.ToLower(out), "error")

		require.Len(t, mockAPI.Pings(), 1)
		cfg, err := config.Load()
		require.NoError(t, err)
		assert.Regexp(t, uuidV4Pattern, cfg.InstallID, "identity persists so the retry reuses the same id")
		assert.Empty(t, cfg.ReportedVersion, "a rejected report must not be recorded")
	})

	t.Run("Completion request sends nothing", func(t *testing.T) {
		env := testutil.SetupTestEnv(t)
		resetFlags()
		pinVersion(t, "1.2.3")
		env.EnableTelemetry()
		mockAPI := testutil.CreateDefaultMockAPI()
		env.SetupMockAPI(mockAPI)

		runComplete(t, "li")
		assert.Empty(t, mockAPI.Pings())
	})
}

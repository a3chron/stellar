package telemetry

import (
	"regexp"
	"runtime"
	"testing"

	"github.com/a3chron/stellar/internal/config"
	"github.com/a3chron/stellar/internal/paths"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var uuidV4 = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

func TestNewInstallID_FormatAndUniqueness(t *testing.T) {
	seen := make(map[string]bool)
	for i := 0; i < 50; i++ {
		id, err := NewInstallID()
		require.NoError(t, err)
		assert.Regexp(t, uuidV4, id)
		assert.False(t, seen[id], "duplicate id %s", id)
		seen[id] = true
	}
}

func TestNormalizeVersion(t *testing.T) {
	assert.Equal(t, "1.4.0", NormalizeVersion("v1.4.0"))
	assert.Equal(t, "1.4.0", NormalizeVersion(" 1.4.0\n"))
	assert.Equal(t, "", NormalizeVersion(""))
	assert.Equal(t, "dev", NormalizeVersion("dev"))
}

func TestDue(t *testing.T) {
	t.Run("first run", func(t *testing.T) {
		assert.True(t, Due("1.4.0", ""))
	})
	t.Run("version changed", func(t *testing.T) {
		assert.True(t, Due("1.4.0", "1.3.0"))
	})
	t.Run("same version", func(t *testing.T) {
		assert.False(t, Due("1.4.0", "1.4.0"))
	})
	t.Run("v prefix does not count as a change", func(t *testing.T) {
		assert.False(t, Due("v1.4.0", "1.4.0"))
	})
}

func TestEnsureIdentity(t *testing.T) {
	t.Run("fresh config becomes an install", func(t *testing.T) {
		cfg := &config.Config{}
		changed, err := EnsureIdentity(cfg, true)
		require.NoError(t, err)
		assert.True(t, changed)
		assert.Regexp(t, uuidV4, cfg.InstallID)
		assert.Equal(t, KindInstall, cfg.InstallKind)
	})

	t.Run("pre-existing config becomes existing", func(t *testing.T) {
		cfg := &config.Config{CurrentTheme: "alice/rainbow@1.0"}
		changed, err := EnsureIdentity(cfg, false)
		require.NoError(t, err)
		assert.True(t, changed)
		assert.Equal(t, KindExisting, cfg.InstallKind)
	})

	t.Run("idempotent once an id exists", func(t *testing.T) {
		cfg := &config.Config{InstallID: "fixed", InstallKind: KindExisting}
		changed, err := EnsureIdentity(cfg, true)
		require.NoError(t, err)
		assert.False(t, changed)
		assert.Equal(t, "fixed", cfg.InstallID)
		assert.Equal(t, KindExisting, cfg.InstallKind, "kind must never be revisited")
	})
}

func TestBuildPing(t *testing.T) {
	cfg := &config.Config{
		InstallID:       "abc",
		InstallKind:     KindInstall,
		ReportedVersion: "1.3.0",
	}
	p := BuildPing(cfg, "v1.4.0", EventReport)
	assert.Equal(t, "abc", p.ID)
	assert.Equal(t, KindInstall, p.Kind)
	assert.Equal(t, EventReport, p.Event)
	assert.Equal(t, "1.4.0", p.Version)
	assert.Equal(t, "1.3.0", p.Previous)
	assert.Equal(t, runtime.GOOS, p.OS)
	assert.Equal(t, runtime.GOARCH, p.Arch)
}

func TestOptedOut(t *testing.T) {
	cases := []struct {
		name       string
		noTelem    string
		doNotTrack string
		want       bool
	}{
		{"neither set", "", "", false},
		{"STELLAR_NO_TELEMETRY=1", "1", "", true},
		{"STELLAR_NO_TELEMETRY=anything", "yes please", "", true},
		{"DO_NOT_TRACK=1", "", "1", true},
		{"DO_NOT_TRACK=0 is not an opt-out", "", "0", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(paths.EnvNoTelemetry, tc.noTelem)
			t.Setenv(paths.EnvDoNotTrack, tc.doNotTrack)
			assert.Equal(t, tc.want, OptedOut())
		})
	}
}

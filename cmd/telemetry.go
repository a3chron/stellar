package cmd

import (
	"time"

	"github.com/a3chron/stellar/internal/api"
	"github.com/a3chron/stellar/internal/config"
	"github.com/a3chron/stellar/internal/telemetry"
)

// The anonymous install report, wired into the root command's hooks.
//
// Two rules shape everything here. First, telemetry must never be felt: it
// never prints, never fails a command, and never makes the user wait - the
// request runs in a goroutine started before RunE and is joined only after
// RunE has produced its output. Second, config.json is written by exactly one
// goroutine at a time: apply/remove/rollback save it during RunE, so the
// background goroutine does network I/O only. The identity is saved on the
// main goroutine BEFORE RunE (so RunE's own Load sees and preserves it), and
// the "reported" marker is saved on the main goroutine AFTER RunE.

// pingOutcome is what the background request hands back to finishTelemetry.
type pingOutcome struct {
	version string
	err     error
}

// pendingPing is set by startTelemetry and drained by finishTelemetry. Both run
// on the main goroutine (PersistentPreRunE / PersistentPostRunE), so it needs
// no locking. Nil when no report is in flight.
var pendingPing <-chan pingOutcome

// joinTimeout bounds how long finishTelemetry waits for the request. The client
// already times out at 2s; this is belt-and-braces so a stuck DNS lookup can
// never hold the process open.
const joinTimeout = 3 * time.Second

// telemetryEnabled gates every report: dev builds never phone home (they are
// somebody's working copy, not an install), and the user can opt out.
func telemetryEnabled() bool {
	return !IsDev() && !telemetry.OptedOut()
}

// startTelemetry is called from PersistentPreRunE once the stellar directory
// exists. configCreated says whether config.json was created on this run,
// which is what decides whether this machine is a clean install.
func startTelemetry(configCreated bool) {
	if !telemetryEnabled() {
		return
	}

	cfg, err := config.Load()
	if err != nil {
		return
	}

	changed, err := telemetry.EnsureIdentity(cfg, configCreated)
	if err != nil {
		return
	}
	if changed {
		// Persist the identity even if the request below fails, so a retry on
		// the next run reports under the same id and kind.
		if err := cfg.Save(); err != nil {
			return
		}
	}

	version := telemetry.NormalizeVersion(versionInfo.version)
	if !telemetry.Due(version, cfg.ReportedVersion) {
		return
	}

	ping := telemetry.BuildPing(cfg, version, telemetry.EventReport)
	ch := make(chan pingOutcome, 1)
	go func() {
		ch <- pingOutcome{version: ping.Version, err: api.NewTelemetryClient().SendCLIPing(ping)}
	}()
	pendingPing = ch
}

// finishTelemetry is called from PersistentPostRunE, after RunE succeeded. It
// records a successful report in config.json; a failed one leaves the config
// untouched so the next run tries again.
func finishTelemetry() {
	ch := pendingPing
	pendingPing = nil
	if ch == nil {
		return
	}

	var out pingOutcome
	select {
	case out = <-ch:
	case <-time.After(joinTimeout):
		return
	}
	if out.err != nil {
		return
	}

	// Re-load rather than reuse the PreRun copy: RunE may have saved a newer
	// config (a newly applied theme, say) that must not be rolled back.
	cfg, err := config.Load()
	if err != nil {
		return
	}
	cfg.ReportedVersion = out.version
	cfg.ReportedAt = time.Now().UTC().Format(time.RFC3339)
	_ = cfg.Save()
}

// cancelTelemetry drops any in-flight report without recording it. Uninstall
// calls it: recording would re-create config.json in a directory the user
// just asked to have removed.
func cancelTelemetry() {
	pendingPing = nil
}

// reportUninstall tells the hub this install is gone. Synchronous, because it
// is the command's whole point rather than a side effect, but still bounded by
// the client's 2s timeout and never fatal. Must run while config.json still
// exists, since the install id lives there.
func reportUninstall(cfg *config.Config) (sent bool) {
	if !telemetryEnabled() || cfg == nil || cfg.InstallID == "" {
		return false
	}
	ping := telemetry.BuildPing(cfg, versionInfo.version, telemetry.EventUninstall)
	return api.NewTelemetryClient().SendCLIPing(ping) == nil
}

// Package telemetry holds the decision logic for stellar's anonymous install
// report: whether to send one, what identity to send it under, and what goes
// in it. It is deliberately free of I/O beyond reading two environment
// variables, so every rule here is unit-testable; the network and config
// writes live in cmd/telemetry.go.
//
// What the hub learns from a report: a random install id, whether stellar was
// freshly installed or already present when reporting began, the CLI version,
// the previously reported version, GOOS and GOARCH. Nothing else - no theme
// names, no paths, no usernames.
package telemetry

import (
	"crypto/rand"
	"fmt"
	"os"
	"runtime"
	"strings"

	"github.com/a3chron/stellar/internal/api"
	"github.com/a3chron/stellar/internal/config"
	"github.com/a3chron/stellar/internal/paths"
)

const (
	// KindInstall marks an install whose config.json did not exist when the
	// id was minted: stellar had never run on this machine before.
	KindInstall = "install"
	// KindExisting marks an install that predates telemetry: config.json was
	// already there, so this is an upgrade of an install the hub never saw.
	KindExisting = "existing"

	// EventReport is the ordinary "I exist / I changed version" report.
	EventReport = "report"
	// EventUninstall tells the hub this install is gone.
	EventUninstall = "uninstall"
)

// OptedOut reports whether the user disabled telemetry via STELLAR_NO_TELEMETRY
// (any non-empty value) or the cross-tool DO_NOT_TRACK=1 convention.
func OptedOut() bool {
	if os.Getenv(paths.EnvNoTelemetry) != "" {
		return true
	}
	return os.Getenv(paths.EnvDoNotTrack) == "1"
}

// NewInstallID returns a random RFC 4122 version-4 UUID. Hand-rolled from
// crypto/rand rather than pulling in a uuid module: it is sixteen bytes and
// two bit twiddles, and the module would be the CLI's only dependency that
// exists for a single call.
func NewInstallID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("failed to generate install id: %w", err)
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // RFC 4122 variant
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}

// NormalizeVersion strips whitespace and a leading "v" so a goreleaser
// version ("1.4.0") and a tag ("v1.4.0") compare equal.
func NormalizeVersion(v string) string {
	return strings.TrimPrefix(strings.TrimSpace(v), "v")
}

// Due reports whether a report should be sent: only when the running version
// is not the one the hub last accepted. There is no time-based heartbeat by
// design - a first run and a version change are the only events worth a
// request for a CLI this small.
func Due(version, reportedVersion string) bool {
	return NormalizeVersion(version) != NormalizeVersion(reportedVersion)
}

// EnsureIdentity gives cfg an install id and kind if it has none yet. The kind
// is decided exactly once, from whether config.json had to be created on this
// run, and never revisited: a report that fails and is retried later must be
// classified the same way as the one that failed. Returns whether cfg changed
// so the caller knows to save it.
func EnsureIdentity(cfg *config.Config, configCreated bool) (changed bool, err error) {
	if cfg.InstallID != "" {
		return false, nil
	}

	id, err := NewInstallID()
	if err != nil {
		return false, err
	}

	cfg.InstallID = id
	if configCreated {
		cfg.InstallKind = KindInstall
	} else {
		cfg.InstallKind = KindExisting
	}
	return true, nil
}

// BuildPing assembles the report for cfg's identity, the running version and
// the given event.
func BuildPing(cfg *config.Config, version, event string) api.CLIPing {
	return api.CLIPing{
		ID:       cfg.InstallID,
		Kind:     cfg.InstallKind,
		Event:    event,
		Version:  NormalizeVersion(version),
		Previous: NormalizeVersion(cfg.ReportedVersion),
		OS:       runtime.GOOS,
		Arch:     runtime.GOARCH,
	}
}

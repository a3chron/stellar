// cmd/hub_errors.go holds the hub-reachability error/notice messages shared
// by apply and preview (and, where the shape matches, info): both resolve a
// theme's version against the hub before doing anything else, and both
// download theme content from the hub, so the same failure (offline, 404)
// should never read differently depending on which command hit it.
package cmd

import (
	"errors"
	"fmt"
	"strings"

	"github.com/a3chron/stellar/internal/api"
	"github.com/a3chron/stellar/internal/theme"
)

// hubUnreachableError builds the fatal error for "couldn't resolve a version
// from the hub, and there's no local cache to fall back to" - used by both
// apply and preview's version-resolution step, and by info's initial
// GetThemeInfo call, so the three of them never disagree on how offline vs.
// genuinely-missing is worded.
func hubUnreachableError(t *theme.Theme, infoErr error) error {
	switch {
	case errors.Is(infoErr, api.ErrOffline):
		return fmt.Errorf("can't reach stellar-hub (are you offline?) and no local cache for %s/%s (details: %v)", t.Author, t.Name, infoErr)
	case errors.Is(infoErr, api.ErrNotFound):
		return fmt.Errorf("no theme %s/%s on stellar-hub", t.Author, t.Name)
	default:
		return fmt.Errorf("theme not found: %s/%s (not available online and no local cache)", t.Author, t.Name)
	}
}

// hubUnavailableNotice builds the informational (non-fatal) message printed
// when the hub couldn't be reached to resolve a version, but a local cache
// exists to fall back to.
func hubUnavailableNotice(t *theme.Theme, infoErr error) string {
	switch {
	case errors.Is(infoErr, api.ErrOffline):
		return "Can't reach stellar-hub (are you offline?), using local cache"
	case errors.Is(infoErr, api.ErrNotFound):
		return fmt.Sprintf("No theme %s/%s on stellar-hub, using local cache", t.Author, t.Name)
	default:
		return "Theme not found online, using local cache"
	}
}

// downloadError builds the error for a failed FetchThemeConfig call, shared
// by apply and preview so a download failure reads identically regardless of
// which command triggered it. On a 404 it makes one extra best-effort
// request to tell "the theme doesn't exist at all" from "the theme exists,
// but not this version" (and lists what does).
func downloadError(client *api.Client, t *theme.Theme, err error) error {
	switch {
	case errors.Is(err, api.ErrOffline):
		return fmt.Errorf("can't reach stellar-hub (are you offline?) (details: %v)", err)
	case errors.Is(err, api.ErrNotFound):
		if info, infoErr := client.GetThemeInfo(t.Author, t.Name); infoErr == nil && len(info.Versions) > 0 {
			vers := make([]string, len(info.Versions))
			for i, v := range info.Versions {
				vers[i] = v.Version
			}
			return fmt.Errorf("version %s not found for %s/%s on stellar-hub (available: %s)", t.Version, t.Author, t.Name, strings.Join(vers, ", "))
		}
		return fmt.Errorf("theme not found: %s/%s@%s", t.Author, t.Name, t.Version)
	default:
		return fmt.Errorf("failed to download: %w", err)
	}
}

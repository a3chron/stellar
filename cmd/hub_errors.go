// cmd/hub_errors.go holds the hub-reachability error/notice messages shared
// by apply and preview (and, where the shape matches, info): both resolve a
// theme's version against the hub before doing anything else, and both
// download theme content from the hub, so the same failure (offline, 404)
// should never read differently depending on which command hit it.
//
// Two of these (themeNotFoundError, versionNotFoundError) also attach
// best-effort "did you mean" suggestions - see cmd/suggest.go for the
// candidate gathering and ranking behind them.
package cmd

import (
	"errors"
	"fmt"
	"strings"

	"github.com/a3chron/stellar/internal/api"
	"github.com/a3chron/stellar/internal/cache"
	"github.com/a3chron/stellar/internal/theme"
)

// hubUnreachableError builds the fatal error for "couldn't resolve a version
// from the hub, and there's no local cache to fall back to" - used by both
// apply and preview's version-resolution step, and by info's initial
// GetThemeInfo call, so the three of them never disagree on how offline vs.
// genuinely-missing is worded. A genuine 404 (the theme doesn't exist, as
// opposed to being unreachable) gets "did you mean" suggestions via
// themeNotFoundError.
func hubUnreachableError(t *theme.Theme, infoErr error) error {
	switch {
	case errors.Is(infoErr, api.ErrOffline):
		return fmt.Errorf("can't reach stellar-hub (are you offline?) and no local cache for %s/%s (details: %v)", t.Author, t.Name, infoErr)
	case errors.Is(infoErr, api.ErrNotFound):
		return themeNotFoundError(t.Author, t.Name)
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
// which command triggered it. cmdName ("apply" or "preview") is used to spell
// out the exact command in a version suggestion.
//
// On a 404 it makes one extra best-effort request to tell "the theme doesn't
// exist at all" (themeNotFoundError, with suggestions) from "the theme
// exists, but not this version" (versionNotFoundError, listing what does and
// suggesting the closest one) - the explicit-version download path (an
// identifier like "author/theme@1.2") skips version *resolution* entirely
// and goes straight here, so this is the only place that distinction is ever
// made for it. When the hub can't be reached at all but this exact theme has
// other versions cached locally, those are used instead of a bare "offline"
// error - a locally-cached theme is real evidence the theme exists, so the
// same not-found-with-suggestions shape applies even offline.
func downloadError(cmdName string, client *api.Client, t *theme.Theme, err error) error {
	switch {
	case errors.Is(err, api.ErrOffline):
		if localVersions, cerr := cache.ListThemeVersions(t.Author, t.Name); cerr == nil && len(localVersions) > 0 {
			return versionNotFoundError(cmdName, t, localVersions)
		}
		return fmt.Errorf("can't reach stellar-hub (are you offline?) (details: %v)", err)
	case errors.Is(err, api.ErrNotFound):
		if info, infoErr := client.GetThemeInfo(t.Author, t.Name); infoErr == nil && len(info.Versions) > 0 {
			vers := make([]string, len(info.Versions))
			for i, v := range info.Versions {
				vers[i] = v.Version
			}
			return versionNotFoundError(cmdName, t, vers)
		}
		return themeNotFoundError(t.Author, t.Name)
	default:
		return fmt.Errorf("failed to download: %w", err)
	}
}

// versionNotFoundError reports that author/name exists but t.Version doesn't,
// listing the versions that do (oldest first, with the latest called out)
// and suggesting the closest one - same major, nearest minor, or the latest
// otherwise (see closestVersion) - as a ready-to-run "stellar <cmdName> ..."
// command. available may come from the hub or, when the hub is unreachable,
// the local cache (see downloadError) - either way it's the authoritative
// answer for what actually exists, so the message never mentions "offline"
// itself.
func versionNotFoundError(cmdName string, t *theme.Theme, available []string) error {
	msg := fmt.Sprintf("%s/%s has no version %s - is that the right version?", t.Author, t.Name, t.Version)

	sorted := sortVersionsAscending(available)
	if len(sorted) == 0 {
		return errors.New(msg)
	}
	latest := sorted[len(sorted)-1]
	msg += fmt.Sprintf("\nAvailable versions: %s (latest: %s)", strings.Join(sorted, ", "), latest)

	if suggestion := closestVersion(t.Version, sorted); suggestion != "" {
		suggested := &theme.Theme{Author: t.Author, Name: t.Name, Version: suggestion, VersionExplicit: true}
		msg += fmt.Sprintf("\nDid you mean: stellar %s %s", cmdName, suggested.String())
	}

	return errors.New(msg)
}

// themeNotFoundError reports that author/slug doesn't exist on the hub at
// all, choosing between two headlines depending on whether the author
// itself has any published themes (gatherThemeSuggestions.authorHasThemes,
// only trusted when authorChecked is true - an unreachable hub must not
// produce a confident "no author" claim), and appending up to
// maxSuggestions ranked "did you mean" candidates gathered from the author's
// other themes, a hub-wide search on the slug, and the local cache.
func themeNotFoundError(author, slug string) error {
	suggestions := gatherThemeSuggestions(author, slug)

	var msg string
	if suggestions.authorChecked && !suggestions.authorHasThemes {
		msg = fmt.Sprintf("no author %s on stellar-hub", author)
	} else {
		msg = fmt.Sprintf("no theme %s/%s on stellar-hub - is that the right theme name?", author, slug)
	}

	switch len(suggestions.candidates) {
	case 0:
		// Nothing close enough to suggest - say so and stop, rather than
		// ever pointing at junk.
	case 1:
		msg += "\nDid you mean: " + suggestions.candidates[0]
	default:
		msg += "\nDid you mean:"
		for _, c := range suggestions.candidates {
			msg += "\n  " + c
		}
	}

	return errors.New(msg)
}

// cmd/author.go holds the "[author/]name" parsing shared by `stellar new`
// and `stellar fork`'s destination argument.
package cmd

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/a3chron/stellar/internal/config"
	"github.com/a3chron/stellar/internal/symlink"
	"github.com/a3chron/stellar/internal/theme"
)

// cachedAuthor resolves the author to use for a locally-created theme when
// none was given explicitly: the username cached from the last successful
// `stellar auth` login (config.Config.Username), falling back to the
// sanitized OS username - the same fallback internal/symlink uses for
// config backups, so a theme and a backup created on the same machine land
// under the same author by default.
func cachedAuthor() string {
	cfg, err := config.Load()
	if err == nil && cfg.Username != "" {
		return cfg.Username
	}
	return symlink.BackupAuthor()
}

// splitAuthoredName parses "[author/]name": a bare name uses cachedAuthor()
// for the author. The author segment must be a valid theme-identifier
// segment (letters, digits, '_', '-') and is kept exactly as typed; the name
// segment is normalized to the hub's slug form (see
// normalizeDestinationName) - `stellar publish` would force it into that
// shape anyway, so a local theme might as well be named that way from the
// start. normalized reports whether name differs from what was actually
// typed, so a caller can tell the user when that happened.
func splitAuthoredName(arg string) (author, name string, normalized bool, err error) {
	var rawName string
	if slash := strings.IndexByte(arg, '/'); slash >= 0 {
		author, rawName = arg[:slash], arg[slash+1:]
	} else {
		author, rawName = cachedAuthor(), arg
	}

	if !theme.IsValidSegment(author) {
		return "", "", false, fmt.Errorf(
			"invalid author %q (may contain only letters, digits, '_' and '-')", author,
		)
	}

	name, err = normalizeDestinationName(rawName)
	if err != nil {
		return "", "", false, err
	}
	return author, name, name != rawName, nil
}

// normalizeDestinationName applies normalizeSlug (cmd/publish.go) to a
// `new`/`fork` destination's theme-name segment, so a locally-created theme
// is always named the way the hub itself would require it to be named on
// publish (lowercase letters, digits and dashes) - rather than letting it
// diverge under a name `stellar publish` would reject or rewrite later.
// Returns an error if nothing usable survives normalization, or if the
// result is the reserved "backup" name.
func normalizeDestinationName(raw string) (string, error) {
	name := normalizeSlug(raw)
	if name == "" {
		return "", fmt.Errorf("%q doesn't leave anything usable as a theme name (lowercase letters, digits and dashes)", raw)
	}
	if name == theme.BackupThemeName {
		return "", fmt.Errorf("%q is reserved for stellar's own config backups - pick another name", name)
	}
	return name, nil
}

// normalizedNameNotice is the dim note shown whenever a `new`/`fork`
// destination's typed name didn't survive normalization unchanged - so the
// user isn't left wondering why the theme landed under a different name
// than what they typed.
func normalizedNameNotice(identifier string) string {
	return fmt.Sprintf("Saved as %s (theme names are lowercase with dashes, like on the hub)", identifier)
}

// ensureFreshThemeDir refuses when dir already holds anything. `new` and
// `fork` only ever start a theme from scratch, so an existing folder is
// somebody's theme - even one without a 1.0.toml (only a 2.0.toml, say) -
// and must not get a stranger's 1.0 mixed into it.
func ensureFreshThemeDir(author, name, dir string) error {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if len(entries) > 0 {
		return fmt.Errorf("%s/%s already exists at %s - pick another name", author, name, dir)
	}
	return nil
}

// writeNewThemeFile creates path and fails if anything already exists
// there. O_EXCL makes the existence check and the write a single step, so
// not even a file that appeared after ensureFreshThemeDir looked is ever
// overwritten.
func writeNewThemeFile(path string, data []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if errors.Is(err, os.ErrExist) {
		return fmt.Errorf("%s already exists - not overwriting it", path)
	}
	if err != nil {
		return err
	}

	_, werr := f.Write(data)
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	if werr != nil {
		// Created a moment ago by this call and incomplete - don't leave a
		// half-written theme behind to block the next attempt.
		_ = os.Remove(path)
		return werr
	}
	return nil
}

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
// for the author. Both segments must be valid theme-identifier segments
// (letters, digits, '_', '-').
func splitAuthoredName(arg string) (author, name string, err error) {
	if slash := strings.IndexByte(arg, '/'); slash >= 0 {
		author, name = arg[:slash], arg[slash+1:]
	} else {
		author, name = cachedAuthor(), arg
	}

	if !theme.IsValidSegment(author) || !theme.IsValidSegment(name) {
		return "", "", fmt.Errorf(
			"invalid theme name %q (author and name may contain only letters, digits, '_' and '-')", arg,
		)
	}
	return author, name, nil
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

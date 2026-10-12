// cmd/author.go holds the "[author/]name" parsing shared by `stellar new`
// and `stellar fork`'s destination argument.
package cmd

import (
	"fmt"
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

package cache

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/a3chron/stellar/internal/paths"
	"github.com/a3chron/stellar/internal/theme"
)

func EnsureCacheDir() error {
	cacheDir, err := paths.StellarHome()
	if err != nil {
		return err
	}
	return os.MkdirAll(cacheDir, 0755)
}

func SaveTheme(t *theme.Theme, content string) error {
	path, err := t.CachePath()
	if err != nil {
		return err
	}

	// Ensure parent directory exists
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}

	return os.WriteFile(path, []byte(content), 0644)
}

func ThemeExists(t *theme.Theme) bool {
	path, err := t.CachePath()
	if err != nil {
		return false
	}

	_, err = os.Stat(path)
	return err == nil
}

func ListCachedThemes() ([]string, error) {
	cacheDir, err := paths.StellarHome()
	if err != nil {
		return nil, err
	}

	var themes []string

	// Walk through author directories
	authors, err := os.ReadDir(cacheDir)
	if err != nil {
		return nil, err
	}

	for _, author := range authors {
		if !author.IsDir() || author.Name() == "config.json" {
			continue
		}

		authorPath := filepath.Join(cacheDir, author.Name())
		themeNames, err := os.ReadDir(authorPath)
		if err != nil {
			continue
		}

		for _, themeName := range themeNames {
			if !themeName.IsDir() {
				continue
			}

			themePath := filepath.Join(authorPath, themeName.Name())
			versions, err := os.ReadDir(themePath)
			if err != nil {
				continue
			}

			for _, version := range versions {
				if filepath.Ext(version.Name()) == ".toml" {
					ver := strings.TrimSuffix(version.Name(), ".toml")
					themes = append(themes, fmt.Sprintf("%s/%s@%s",
						author.Name(), themeName.Name(), ver))
				}
			}
		}
	}

	return themes, nil
}

// CleanOptions controls what CleanCache (and PlanClean) consider removable.
// ~/.config/stellar is both a download cache and where the README tells
// people to keep their own themes, so the two have to be told apart: see
// isDownloadOnly for exactly how.
type CleanOptions struct {
	// KeepPath is the cache path of the currently applied theme. It is kept
	// regardless of every other rule below, unless All is set.
	KeepPath string
	// All also removes the currently applied theme (KeepPath is ignored).
	All bool
	// Downloaded lists "author/name" themes stellar itself downloaded
	// (config.Config.DownloadedThemes).
	Downloaded []string
	// Authored lists "author/name" themes created locally via `stellar
	// new`/`stellar fork` (config.Config.AuthoredThemes). A theme in both
	// Downloaded and Authored - published, then re-downloaded - still
	// counts as "own", not a plain download: see isDownloadOnly.
	Authored []string
	// IncludeOwn also removes "own" themes: anything isDownloadOnly doesn't
	// recognize as a plain download (hand-made themes, `new`/`fork` output,
	// and anything fetched by a stellar version from before this
	// distinction existed).
	IncludeOwn bool
}

// CleanResult reports what CleanCache actually removed, split the same way
// the command's own output is: downloads never need a second thought, own
// themes are the part a user might want to know got removed.
type CleanResult struct {
	DownloadedRemoved int
	OwnRemoved        int
}

// OwnTheme summarizes one "own" author/name for the --include-own
// confirmation prompt and the "kept" summary.
type OwnTheme struct {
	ID       string // "author/name"
	Versions int
}

// CleanPlan is what CleanCache would do, computed without touching the
// filesystem - used to build `stellar clean`'s --include-own confirmation
// and its "kept" summary.
type CleanPlan struct {
	// DownloadedCount is how many cached download files would be removed -
	// this happens regardless of IncludeOwn, so it never needs a confirm.
	DownloadedCount int
	// OwnThemes are the own themes IncludeOwn would remove, sorted by ID.
	// Computed the same whether or not Options.IncludeOwn is actually set,
	// so a caller can preview it before deciding.
	OwnThemes []OwnTheme
	// BackupVersions is how many <author>/backup version files are cached -
	// never removed by any CleanCache variant, only by `stellar remove
	// <author>/backup`.
	BackupVersions int
}

// isDownloadOnly reports whether themeID ("author/name") counts as a plain
// download stellar can always re-fetch from the hub: present in downloaded,
// and NOT also in authored. A theme you published yourself and later
// re-downloaded (e.g. on another machine) stays "yours" even though it's
// technically also a download - re-running `stellar clean` must not treat
// your own published theme as disposable just because you also have it
// through `stellar apply`.
func isDownloadOnly(themeID string, downloaded, authored map[string]bool) bool {
	return downloaded[themeID] && !authored[themeID]
}

func toSet(items []string) map[string]bool {
	set := make(map[string]bool, len(items))
	for _, item := range items {
		set[item] = true
	}
	return set
}

// cachedThemeFile is one classified entry from ListCachedThemes, shared by
// CleanCache and PlanClean so the two can never disagree on what counts as
// removable.
type cachedThemeFile struct {
	path      string
	themeID   string // "author/name"
	isBackup  bool
	isOwn     bool // not isDownloadOnly
	isCurrent bool // path == opts.KeepPath
}

// classifyCachedThemes walks every cached theme version and classifies it
// against opts, without touching the filesystem beyond the read already
// done by ListCachedThemes.
func classifyCachedThemes(opts CleanOptions) ([]cachedThemeFile, error) {
	themes, err := ListCachedThemes()
	if err != nil {
		return nil, err
	}

	downloaded := toSet(opts.Downloaded)
	authored := toSet(opts.Authored)

	out := make([]cachedThemeFile, 0, len(themes))
	for _, themeID := range themes {
		t, err := theme.ParseIdentifier(themeID)
		if err != nil {
			continue
		}
		path, err := t.CachePath()
		if err != nil {
			continue
		}
		id := fmt.Sprintf("%s/%s", t.Author, t.Name)
		out = append(out, cachedThemeFile{
			path:      path,
			themeID:   id,
			isBackup:  t.Name == theme.BackupThemeName,
			isOwn:     !isDownloadOnly(id, downloaded, authored),
			isCurrent: opts.KeepPath != "" && path == opts.KeepPath,
		})
	}
	return out, nil
}

// PlanClean computes what CleanCache(opts) would do without touching the
// filesystem, for `stellar clean`'s --include-own confirmation and its
// "kept" summary. OwnThemes and BackupVersions are the same regardless of
// opts.IncludeOwn, so a caller can ask "what WOULD --include-own remove"
// before deciding whether to actually pass it.
func PlanClean(opts CleanOptions) (CleanPlan, error) {
	files, err := classifyCachedThemes(opts)
	if err != nil {
		return CleanPlan{}, err
	}

	var plan CleanPlan
	ownVersions := make(map[string]int)

	for _, f := range files {
		if f.isBackup {
			plan.BackupVersions++
			continue
		}
		if !opts.All && f.isCurrent {
			continue
		}
		if f.isOwn {
			ownVersions[f.themeID]++
		} else {
			plan.DownloadedCount++
		}
	}

	ids := make([]string, 0, len(ownVersions))
	for id := range ownVersions {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		plan.OwnThemes = append(plan.OwnThemes, OwnTheme{ID: id, Versions: ownVersions[id]})
	}

	return plan, nil
}

// CleanCache removes cached theme files per opts: backups and (unless All)
// the currently applied theme are always kept; a plain download is always
// removed; an "own" theme (see isDownloadOnly) is removed only when
// IncludeOwn is set.
func CleanCache(opts CleanOptions) (CleanResult, error) {
	files, err := classifyCachedThemes(opts)
	if err != nil {
		return CleanResult{}, err
	}

	var result CleanResult
	dirsToCheck := make(map[string]bool)

	for _, f := range files {
		// Backups of the user's original config live at <author>/backup
		// (see internal/symlink.backupOriginalConfig) and must survive
		// cleaning: they're the only copy of whatever starship.toml the user
		// had before ever running stellar. If clean swept them up, the
		// restore hint stellar itself prints ("stellar apply
		// <author>/backup@<version>") would become permanently
		// unrecoverable, since "backup" isn't a real author on the hub and
		// there's nothing to re-download. Backups are still removable
		// explicitly via `stellar remove <author>/backup[@version]`.
		if f.isBackup {
			continue
		}

		if !opts.All && f.isCurrent {
			continue
		}

		if f.isOwn && !opts.IncludeOwn {
			continue
		}

		if err := os.Remove(f.path); err != nil {
			log.Printf("warning: failed to remove %s: %v", f.path, err)
			continue
		}
		if f.isOwn {
			result.OwnRemoved++
		} else {
			result.DownloadedRemoved++
		}

		// Track parent directories for cleanup
		themeDir := filepath.Dir(f.path) // e.g., ~/.config/stellar/author/theme
		dirsToCheck[themeDir] = true
	}

	// Clean up empty theme and author directories
	for themeDir := range dirsToCheck {
		// Remove theme directory if empty
		if isEmpty, _ := isDirEmpty(themeDir); isEmpty {
			if err := os.Remove(themeDir); err != nil {
				log.Printf("warning: failed to remove directory %s: %v", themeDir, err)
			}

			// Also try to remove author directory if empty
			authorDir := filepath.Dir(themeDir)
			if isEmpty, _ := isDirEmpty(authorDir); isEmpty {
				if err := os.Remove(authorDir); err != nil {
					log.Printf("warning: failed to remove directory %s: %v", authorDir, err)
				}
			}
		}
	}

	return result, nil
}

// ListAuthors returns the names of author directories under the stellar
// cache (e.g. ["alice", "bob"]), sorted, skipping non-directories and
// config.json. Returns (nil, nil) if the stellar home directory doesn't
// exist yet: shell completion (internal/completion) calls this on every
// keystroke and cobra doesn't guarantee the directory has been created by
// the time "__complete" runs, so a missing cache must read as "no authors"
// rather than an error.
func ListAuthors() ([]string, error) {
	cacheDir, err := paths.StellarHome()
	if err != nil {
		return nil, err
	}

	entries, err := os.ReadDir(cacheDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var authors []string
	for _, e := range entries {
		if !e.IsDir() || e.Name() == "config.json" {
			continue
		}
		authors = append(authors, e.Name())
	}

	sort.Strings(authors)
	return authors, nil
}

// ListAuthorThemes returns the theme slug directories cached under author
// (e.g. ["rainbow", "sunset"]), sorted. Returns (nil, nil) if the author
// directory doesn't exist (see ListAuthors for why that's not an error).
func ListAuthorThemes(author string) ([]string, error) {
	cacheDir, err := paths.StellarHome()
	if err != nil {
		return nil, err
	}

	entries, err := os.ReadDir(filepath.Join(cacheDir, author))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var slugs []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		slugs = append(slugs, e.Name())
	}

	sort.Strings(slugs)
	return slugs, nil
}

// ListThemeVersions returns the cached version strings for author/name,
// newest first (theme.CompareSemver, descending), e.g. ["1.2", "1.1", "1.0"].
// Returns (nil, nil) if the theme directory doesn't exist (see ListAuthors
// for why that's not an error).
func ListThemeVersions(author, name string) ([]string, error) {
	cacheDir, err := paths.StellarHome()
	if err != nil {
		return nil, err
	}

	entries, err := os.ReadDir(filepath.Join(cacheDir, author, name))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var versions []string
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".toml" {
			continue
		}
		versions = append(versions, strings.TrimSuffix(e.Name(), ".toml"))
	}

	sort.Slice(versions, func(i, j int) bool {
		return theme.CompareSemver(versions[i], versions[j]) > 0
	})

	return versions, nil
}

func TmpCachePath(t *theme.Theme) string {
	return paths.TmpThemePath(t.Author, t.Name, t.Version)
}

func TmpCacheDir(t *theme.Theme) string {
	return paths.TmpThemeDir(t.Author, t.Name)
}

func TmpThemeExists(t *theme.Theme) bool {
	_, err := os.Stat(TmpCachePath(t))
	return err == nil
}

func SaveThemeToTmp(t *theme.Theme, content string) error {
	path := TmpCachePath(t)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0644)
}

func isDirEmpty(path string) (bool, error) {
	entries, err := os.ReadDir(path)
	if err != nil {
		return false, err
	}
	return len(entries) == 0, nil
}

// cmd/publish.go implements `stellar publish`: upload a local theme to
// stellar-hub as a draft (or straight to a finished theme/version with
// --publish), following the decision order from the plan's "C4" section.
package cmd

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"

	"github.com/a3chron/stellar/internal/api"
	"github.com/a3chron/stellar/internal/auth"
	"github.com/a3chron/stellar/internal/cache"
	"github.com/a3chron/stellar/internal/completion"
	"github.com/a3chron/stellar/internal/config"
	"github.com/a3chron/stellar/internal/paths"
	"github.com/a3chron/stellar/internal/term"
	"github.com/a3chron/stellar/internal/theme"
	"github.com/fatih/color"
	"github.com/spf13/cobra"
)

var (
	publishSlug        string
	publishName        string
	publishDescription string
	publishGroup       string
	publishColorMode   string
	publishDeps        []string
	publishMinStarship string
	publishScreenshot  string
	publishVersion     string
	publishNotes       string
	publishDraftCode   string
	publishDoPublish   bool
	publishYes         bool
)

var publishCmd = &cobra.Command{
	Use:   "publish <path|author/slug[@version]|name>",
	Short: "Upload a local theme to stellar-hub",
	Long: `Upload a local theme to stellar-hub as a draft, which you finish on the web -
or pass --publish to finish it immediately from the CLI.

The argument can be a path to a .toml file or a theme directory, an
"author/slug[@version]" identifier for a theme you already have cached, or
just a bare theme name under your stellar cache.

If the theme isn't on the hub yet, this starts a new theme. If it's already
yours, it starts a new version instead - "stellar publish" is used for both.`,
	Example: `  stellar publish my-theme
  stellar publish ./starship.toml --slug my-theme
  stellar publish a3chron/my-theme --publish
  stellar publish my-theme --draft K7QF2M`,
	Args: argsWithUsage(cobra.ExactArgs(1)),
	RunE: runPublish,
}

func init() {
	publishCmd.Flags().StringVar(&publishSlug, "slug", "", "Slug to publish under (default: derived from the folder/file name)")
	publishCmd.Flags().StringVar(&publishName, "name", "", "Theme display name (new themes only)")
	publishCmd.Flags().StringVar(&publishDescription, "description", "", "Theme description (new themes only)")
	publishCmd.Flags().StringVar(&publishGroup, "group", "", "Group for related variants (new themes only)")
	publishCmd.Flags().StringVar(&publishColorMode, "color-mode", "", "dark, light, or both (new themes only)")
	publishCmd.Flags().StringArrayVar(&publishDeps, "dep", nil, "Something the theme needs, e.g. \"JetBrainsMono Nerd Font\" (repeatable)")
	publishCmd.Flags().StringVar(&publishMinStarship, "min-starship", "", "Minimum Starship version, e.g. 1.24.0")
	publishCmd.Flags().StringVar(&publishScreenshot, "screenshot", "", "Path to a screenshot (PNG/JPEG/WebP, max 4MB)")
	publishCmd.Flags().StringVar(&publishVersion, "version", "", "Version to publish (default: from the file, or auto-bumped)")
	publishCmd.Flags().StringVar(&publishNotes, "notes", "", "Version notes")
	publishCmd.Flags().StringVar(&publishDraftCode, "draft", "", "Attach to an existing draft by its code")
	publishCmd.Flags().BoolVar(&publishDoPublish, "publish", false, "Finish and publish immediately instead of leaving a draft")
	publishCmd.Flags().BoolVarP(&publishYes, "yes", "y", false,
		"Never prompt: confirm everything and use defaults (fails if a required value has no default)")

	publishCmd.ValidArgsFunction = publishIdentifierArgs
}

// publishIdentifierArgs completes local theme identifiers but, unlike
// themeIdentifierArgs, keeps file completion available too (publish also
// takes a bare path).
func publishIdentifierArgs(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	candidates, _ := completion.ThemeIdentifier(toComplete, completion.LocalOnly)
	return candidates, cobra.ShellCompDirectiveDefault
}

// --- flag validation ---------------------------------------------------------

var (
	minStarshipFlagRe = regexp.MustCompile(`^\d+\.\d+\.\d+$`)
	versionFlagRe     = regexp.MustCompile(`^(0|[1-9]\d*)\.(0|[1-9]\d*)$`)
	draftCodeRe       = regexp.MustCompile(`^[2-9A-HJ-NP-Z]{6}$`)
)

// validatePublishFlags checks every flag's shape before anything else runs -
// no file is read, no request is made - so a typo is reported immediately
// and by name, rather than surfacing later as a confusing 400 from the hub
// (or, for --draft, a request for a code that was never going to be valid).
func validatePublishFlags() error {
	if publishColorMode != "" {
		switch publishColorMode {
		case "dark", "light", "both":
		default:
			return fmt.Errorf("--color-mode must be dark, light, or both (got %q)", publishColorMode)
		}
	}
	if publishMinStarship != "" && !minStarshipFlagRe.MatchString(publishMinStarship) {
		return fmt.Errorf("--min-starship must look like X.Y.Z (got %q)", publishMinStarship)
	}
	if publishVersion != "" && !versionFlagRe.MatchString(normalizeVersionFlag(publishVersion)) {
		return fmt.Errorf("--version must look like X.Y (got %q)", publishVersion)
	}
	if publishDraftCode != "" && !draftCodeRe.MatchString(strings.ToUpper(publishDraftCode)) {
		return fmt.Errorf("--draft must be a 6-character draft code, e.g. K7QF2M (got %q)", publishDraftCode)
	}
	return nil
}

func runPublish(cmd *cobra.Command, args []string) error {
	if err := validatePublishFlags(); err != nil {
		return &usageError{err: err}
	}

	r := newPromptReader(cmd)
	tty := isTerminal(cmd.InOrStdin())
	// --yes means never prompt, full stop: every value prompt below is
	// gated on "interactive", not raw tty, so --yes behaves exactly like a
	// non-interactive run even when stdin genuinely is a terminal.
	// Confirmations are unaffected here - they already check publishYes
	// themselves and answer yes outright.
	interactive := tty && !publishYes

	src, err := resolvePublishSource(r, interactive, args[0])
	if err != nil {
		return err
	}
	fmt.Printf("Using %s\n", src.path)

	// Recorded before any network call: a theme stellar never fetched from
	// anywhere must never be mistaken for a plain download (which
	// `apply --update`/`clean` would otherwise treat it as) just because it
	// happens to be getting published.
	if src.author != "" && src.name != "" {
		if merr := markSourceAuthored(src); merr != nil {
			color.Yellow("Warning: couldn't record %s/%s as authored (stellar clean may remove it): %v", src.author, src.name, merr)
		}
	}

	content, validation, err := loadAndCheckConfig(src.path)
	if err != nil {
		return err
	}
	if validation.HasCustomCommands {
		color.Yellow("[custom] notice: this theme runs shell commands - users will see a security warning when applying it.")
	}

	var screenshotData []byte
	if publishScreenshot != "" {
		screenshotData, err = loadScreenshot(publishScreenshot)
		if err != nil {
			return err
		}
	}

	me, token, err := publishWhoami(cmd, r)
	if err != nil {
		return err
	}

	cfgData, cerr := config.Load()
	if cerr != nil {
		cfgData = &config.Config{}
	}
	// Case-insensitively, matching how the hub itself compares usernames.
	if src.author != "" && !strings.EqualFold(src.author, me.Username) {
		themeID := fmt.Sprintf("%s/%s", src.author, src.name)
		if cfgData.HasDownloaded(themeID) {
			return fmt.Errorf("%s is %s's theme - use `stellar fork %s` to make your own editable copy", themeID, src.author, themeID)
		}
	}

	// --draft skips slug resolution entirely: the draft's identity is
	// already fixed by its code, and deriving (or worse, prompting for) a
	// slug that's never actually used would be pure noise - this is exactly
	// what the wizard's own "stellar publish ./path/to/starship.toml --draft
	// CODE" suggestion needs to just work non-interactively.
	var slug string
	if publishDraftCode == "" {
		slug, err = resolvePublishSlug(r, interactive, src)
		if err != nil {
			return err
		}
		if slug == theme.BackupThemeName {
			return fmt.Errorf("the slug %q is reserved for stellar's own config backups - pick another with --slug", slug)
		}
	}

	client := api.NewAuthedClient(token)
	draft, err := resolveOrCreateDraft(r, interactive, client, me, slug, src, content)
	if err != nil {
		return err
	}

	if len(screenshotData) > 0 {
		if draft.Kind == api.DraftKindVersion {
			color.Yellow("Note: version drafts use the theme's existing screenshot - skipping --screenshot.")
		} else {
			updated, uerr := client.UploadScreenshot(draft.Code, filepath.Base(publishScreenshot), screenshotData)
			if uerr != nil {
				return draftSavedHint(wrapHubAPIError(uerr), draft)
			}
			draft = updated
		}
	}

	if !publishDoPublish {
		printDraftSummary(draft, me.Username)
		return nil
	}

	draft, err = fillMissingForPublish(r, interactive, client, draft)
	if err != nil {
		return draftSavedHint(err, draft)
	}

	result, perr := client.PublishDraft(draft.Code)
	if perr != nil {
		return draftSavedHint(wrapHubAPIError(perr), draft)
	}

	author := term.SanitizeForTerminal(result.Author)
	resultSlug := term.SanitizeForTerminal(result.Slug)
	version := term.SanitizeForTerminal(result.Version)
	color.Green("✓ Published %s/%s@%s", author, resultSlug, version)
	fmt.Printf("  %s%s\n", term.Hyperlink(result.URL, result.URL), color.HiBlackString(term.ClickHint()))
	return nil
}

// markSourceAuthored records "author/name" as authored (config.Config.
// AuthoredThemes) before any network call - see runPublish. It deliberately
// does NOT fall back to an empty *config.Config on a load failure: saving
// one over a config.json that merely failed to *read* would silently wipe
// the user's real current/downloaded-theme state.
func markSourceAuthored(src *publishSource) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	themeID := fmt.Sprintf("%s/%s", src.author, src.name)
	if cfg.HasAuthored(themeID) {
		return nil
	}
	cfg.MarkAuthored(themeID)
	return cfg.Save()
}

// --- source resolution -----------------------------------------------------

// publishSource is the file publish resolved to, plus whatever stellar
// cache metadata could be derived from its location.
type publishSource struct {
	path string
	// author/name are set when path lives under <StellarHome>/<author>/<name>/...
	author, name string
	// version is set when the filename itself is a version ("1.2.toml").
	version string
}

var versionFilenameRe = regexp.MustCompile(`^(0|[1-9]\d*)\.(0|[1-9]\d*)$`)

// resolvePublishSource implements the argument precedence: an existing
// filesystem path first (symlinks resolved, a directory picks its highest
// version or falls back to starship.toml), then an "author/slug[@version]"
// identifier already in the local cache, then a bare name searched across
// every local author.
func resolvePublishSource(r *bufio.Reader, interactive bool, arg string) (*publishSource, error) {
	if path, isPath, err := resolveExistingPath(arg); isPath {
		if err != nil {
			return nil, err
		}
		return sourceFromPath(path)
	}

	if t, perr := theme.ParseIdentifier(arg); perr == nil {
		dir, derr := t.CacheDir()
		if derr != nil {
			return nil, derr
		}
		if t.VersionExplicit && t.Version != theme.LatestVersion {
			p, perr2 := t.CachePath()
			if perr2 == nil {
				if _, statErr := os.Stat(p); statErr == nil {
					return sourceFromPath(p)
				}
			}
		} else if ver, verr := theme.FindLatestLocalVersion(dir); verr == nil {
			p, perr2 := paths.ThemeCachePath(t.Author, t.Name, ver)
			if perr2 != nil {
				return nil, perr2
			}
			return sourceFromPath(p)
		}
		return nil, fmt.Errorf("%s isn't in your local stellar cache - stellar publish only uploads themes you already have locally", t.String())
	}

	matches, merr := findLocalThemeDirsByName(arg)
	if merr != nil {
		return nil, merr
	}
	if len(matches) == 0 {
		// `new`/`fork` normalize a theme's name to slug form (lowercase,
		// dashes) before ever creating its directory, so a bare name typed
		// the way it was BEFORE normalization (e.g. "My_Theme" for a theme
		// actually saved as "my-theme") would otherwise find nothing even
		// though the theme is right there under its normalized name.
		if normalized := normalizeSlug(arg); normalized != "" && normalized != arg {
			matches, merr = findLocalThemeDirsByName(normalized)
			if merr != nil {
				return nil, merr
			}
		}
	}
	if len(matches) == 0 {
		return nil, fmt.Errorf("%q is not a path, a cached author/slug, or a theme name under your stellar cache", arg)
	}

	chosen := matches[0]
	if len(matches) > 1 {
		if !interactive {
			return nil, noAnswerError("the full path to the theme file")
		}
		labels := make([]string, len(matches))
		for i, m := range matches {
			labels[i] = fmt.Sprintf("%s/%s", m.author, m.name)
		}
		idx, serr := promptSelect(r, "Multiple local themes match that name - which one?", labels, -1, "the full path to the theme file")
		if serr != nil {
			return nil, serr
		}
		chosen = matches[idx]
	}

	ver, verr := theme.FindLatestLocalVersion(chosen.dir)
	if verr != nil {
		return nil, fmt.Errorf("no version files found in %s", chosen.dir)
	}
	p, perr := paths.ThemeCachePath(chosen.author, chosen.name, ver)
	if perr != nil {
		return nil, perr
	}
	return sourceFromPath(p)
}

// highestVersionedTomlInDir returns the path of the highest "<X.Y>.toml"
// file directly in dir, or "" if none exist. Unlike
// theme.FindLatestLocalVersion (which treats ANY *.toml filename as a
// candidate and falls back to plain lexicographic ordering for the ones
// that aren't numeric), this never lets an unrelated file outrank the
// theme's own versioned config just because its name happens to sort later
// - a "zellij.toml" sitting next to "starship.toml" must never be picked
// over it.
func highestVersionedTomlInDir(dir string) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}
	best := ""
	bestStem := ""
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		stem := strings.TrimSuffix(e.Name(), ".toml")
		if stem == e.Name() || !versionFilenameRe.MatchString(stem) {
			continue
		}
		if best == "" || theme.CompareSemver(stem, bestStem) > 0 {
			best = filepath.Join(dir, e.Name())
			bestStem = stem
		}
	}
	return best, nil
}

// resolveExistingPath reports whether arg is (or resolves, via symlinks, to)
// an existing file or directory. isPath is true whenever arg looks like a
// path attempt at all (it exists on disk) - a directory with nothing usable
// in it is still isPath=true, with err set, since nothing else in the
// precedence order would make sense of it either.
//
// For a directory, only a proper "<X.Y>.toml" (the highest one) or
// "starship.toml" is ever picked - never some other, unrelated .toml file
// that merely happens to sort after those names.
func resolveExistingPath(arg string) (path string, isPath bool, err error) {
	resolved := arg
	if r, everr := filepath.EvalSymlinks(arg); everr == nil {
		resolved = r
	}

	info, statErr := os.Stat(resolved)
	if statErr != nil {
		return "", false, nil
	}
	if !info.IsDir() {
		return resolved, true, nil
	}

	if best, berr := highestVersionedTomlInDir(resolved); berr == nil && best != "" {
		return best, true, nil
	}
	starship := filepath.Join(resolved, "starship.toml")
	if _, serr := os.Stat(starship); serr == nil {
		return starship, true, nil
	}
	return "", true, fmt.Errorf("%s is a directory with no versioned .toml or starship.toml in it", arg)
}

// sourceFromPath builds a publishSource from a resolved file path, filling
// in author/name/version when the path lives inside StellarHome in the
// usual <author>/<name>/<version>.toml layout.
func sourceFromPath(path string) (*publishSource, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}

	src := &publishSource{path: abs}

	if stem := strings.TrimSuffix(filepath.Base(abs), ".toml"); versionFilenameRe.MatchString(stem) {
		src.version = stem
	}

	// Resolve symlinks on BOTH sides before comparing: StellarHome itself
	// can be a dotfiles symlink, but so can any ancestor of abs (macOS's
	// /tmp -> /private/tmp, a bind-mounted or symlinked $HOME, ...).
	// Comparing one resolved path against one unresolved path would
	// otherwise never match, silently losing the author/name this theme
	// lives under - and with it, the "someone else's theme - use fork"
	// refusal below in runPublish, which keys off src.author being set.
	resolvedAbs := abs
	if r, everr := filepath.EvalSymlinks(abs); everr == nil {
		resolvedAbs = r
	}

	if home, herr := paths.StellarHome(); herr == nil {
		if resolvedHome, everr := filepath.EvalSymlinks(home); everr == nil {
			home = resolvedHome
		}
		if rel, relErr := filepath.Rel(home, resolvedAbs); relErr == nil && !strings.HasPrefix(rel, "..") && rel != "." {
			parts := strings.Split(filepath.ToSlash(rel), "/")
			if len(parts) >= 2 {
				src.author, src.name = parts[0], parts[1]
			}
		}
	}

	return src, nil
}

type localThemeMatch struct {
	author, name, dir string
}

// findLocalThemeDirsByName searches every local author for a theme slug
// matching name.
func findLocalThemeDirsByName(name string) ([]localThemeMatch, error) {
	authors, err := cache.ListAuthors()
	if err != nil {
		return nil, err
	}

	var matches []localThemeMatch
	for _, author := range authors {
		slugs, serr := cache.ListAuthorThemes(author)
		if serr != nil {
			continue
		}
		for _, slug := range slugs {
			if slug != name {
				continue
			}
			dir, derr := paths.ThemeCacheDir(author, slug)
			if derr != nil {
				continue
			}
			matches = append(matches, localThemeMatch{author: author, name: slug, dir: dir})
		}
	}
	return matches, nil
}

// --- local content checks ---------------------------------------------------

// maxConfigUTF16Units is the contract's config size cap: 1-100000 UTF-16
// code units (the same unit JavaScript's String.length counts in on the
// hub), not bytes or runes.
const maxConfigUTF16Units = 100000

// loadAndCheckConfig reads path, normalizes line endings and strips a BOM
// (every write to the hub happens through this exact content), validates it
// as TOML, and enforces the contract's size cap.
func loadAndCheckConfig(path string) (string, theme.ValidationResult, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", theme.ValidationResult{}, err
	}

	content := strings.ReplaceAll(string(raw), "\r\n", "\n")
	content = strings.TrimPrefix(content, "\ufeff")

	result, verr := theme.ValidateConfigContent(content)
	if verr != nil {
		return "", theme.ValidationResult{}, fmt.Errorf("validation error: %w", verr)
	}
	if !result.Valid {
		return "", theme.ValidationResult{}, result.Error
	}

	n := utf16Length(content)
	switch {
	case n == 0:
		return "", theme.ValidationResult{}, fmt.Errorf("config is empty")
	case n > maxConfigUTF16Units:
		return "", theme.ValidationResult{}, fmt.Errorf("config is too long: %d characters (max %d)", n, maxConfigUTF16Units)
	}

	return content, result, nil
}

func utf16Length(s string) int {
	return len(utf16.Encode([]rune(s)))
}

// loadScreenshot reads and validates a screenshot file against the same
// rules the hub enforces, so a doomed upload never has to make a round trip
// to find out.
func loadScreenshot(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("couldn't read screenshot: %w", err)
	}
	base := filepath.Base(path)
	if api.DetectImageContentType(data) == "" {
		return nil, fmt.Errorf("%s doesn't look like a PNG, JPEG or WebP image", base)
	}
	if len(data) > api.MaxScreenshotBytes {
		return nil, fmt.Errorf("%s is %s - screenshots can be at most %s", base, api.FormatMB(len(data)), api.FormatMB(api.MaxScreenshotBytes))
	}
	return data, nil
}

// normalizeUserPath cleans up a path typed - or drag-and-dropped - at a
// prompt: surrounding whitespace, a pair of matching quotes a terminal's
// drag-and-drop often adds around the path, and a leading "~/" (no shell is
// expanding it here, since this never goes through one).
func normalizeUserPath(raw string) string {
	s := strings.TrimSpace(raw)
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			s = s[1 : len(s)-1]
		}
	}
	if strings.HasPrefix(s, "~/") {
		if home, herr := os.UserHomeDir(); herr == nil {
			s = filepath.Join(home, s[2:])
		}
	}
	return s
}

// maxScreenshotPromptAttempts caps how many times the interactive
// screenshot-path prompt retries a bad answer (missing file, wrong
// type/extension, too large) before giving up - a typo'd path shouldn't end
// the whole publish run on the first try, but it also shouldn't loop
// forever: after this many bad paths in a row, the error itself (naming
// --screenshot) is more useful than another prompt.
const maxScreenshotPromptAttempts = 3

// promptForScreenshotWithRetries asks for a screenshot path, uploads it, and
// retries on a bad path (loadScreenshot failing) up to
// maxScreenshotPromptAttempts times - printing the problem each time so a
// typo doesn't read as the prompt silently re-asking the same question. A
// hub-side upload failure (wrapHubAPIError) is not retried here: that's not
// something a different local path would fix.
func promptForScreenshotWithRetries(r *bufio.Reader, client *api.Client, code string) (*api.Draft, error) {
	var lastErr error
	for attempt := 1; attempt <= maxScreenshotPromptAttempts; attempt++ {
		rawPath, err := promptText(r, "Path to a screenshot (PNG/JPEG/WebP, max 4MB)", "", "--screenshot <path>")
		if err != nil {
			return nil, err
		}
		path := normalizeUserPath(rawPath)
		data, lerr := loadScreenshot(path)
		if lerr != nil {
			lastErr = lerr
			if attempt < maxScreenshotPromptAttempts {
				color.Yellow("%v", lerr)
				continue
			}
			return nil, lastErr
		}
		updated, uerr := client.UploadScreenshot(code, filepath.Base(path), data)
		if uerr != nil {
			return nil, wrapHubAPIError(uerr)
		}
		return updated, nil
	}
	return nil, lastErr
}

// --- identity ----------------------------------------------------------------

// publishWhoami resolves the logged-in user, offering an inline login when
// stdin is a terminal and nothing (valid) is stored - rather than making the
// user run `stellar auth` themselves and retype the publish command. --yes
// is treated the same as "no TTY": there is no sensible default for "should
// I interactively log you in", and a device login needs a human in a
// browser anyway, which --yes explicitly asked to avoid.
func publishWhoami(cmd *cobra.Command, r *bufio.Reader) (*api.WhoamiResponse, string, error) {
	baseURL := authBaseURL()
	token, err := auth.Token(baseURL)
	if err != nil {
		return nil, "", err
	}

	if token != "" {
		who, werr := whoamiWithToken(token)
		if werr == nil {
			return who, token, nil
		}
		if !errors.Is(werr, api.ErrUnauthorized) {
			return nil, "", wrapHubAPIError(werr)
		}
		// Falls through: the stored token was revoked/invalid.
	}

	tty := isTerminal(cmd.InOrStdin())
	if !tty || publishYes {
		return nil, "", notLoggedInError()
	}

	ok, perr := promptYesNo(r, tty, "You're not logged in to stellar-hub. Log in now?", true, "stellar auth")
	if perr != nil {
		return nil, "", perr
	}
	if !ok {
		return nil, "", notLoggedInError()
	}

	if err := runAuthLogin(cmd, nil); err != nil {
		return nil, "", err
	}

	token, err = auth.Token(baseURL)
	if err != nil {
		return nil, "", err
	}
	who, werr := whoamiWithToken(token)
	if werr != nil {
		return nil, "", wrapHubAPIError(werr)
	}
	return who, token, nil
}

// --- slug resolution ---------------------------------------------------------

var slugInvalidRe = regexp.MustCompile(`[^a-z0-9-]+`)
var slugCollapseRe = regexp.MustCompile(`-+`)

// normalizeSlug reduces raw to the contract's slug alphabet: lowercase,
// underscores (and any other disallowed character) become '-', runs of '-'
// collapse to one, leading/trailing '-' are trimmed, and the result is
// capped at 50 characters.
func normalizeSlug(raw string) string {
	s := strings.ToLower(raw)
	s = strings.ReplaceAll(s, "_", "-")
	s = slugInvalidRe.ReplaceAllString(s, "-")
	s = slugCollapseRe.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if len(s) > 50 {
		s = strings.TrimRight(s[:50], "-")
	}
	return s
}

// resolvePublishSlug picks the slug to publish under: --slug wins outright;
// otherwise a theme already identified by its stellar-cache location keeps
// its directory name (normalized); a versioned "<X.Y>.toml" outside the
// cache also uses its containing folder's name (the same "theme folder"
// convention); a loose, arbitrary "whatever.toml" uses the file's own name
// instead of whichever folder it happens to sit in (publishing
// ~/Downloads/catppuccin.toml must not become slug "downloads"); only a
// bare "starship.toml" - which carries no name of its own - is asked for
// explicitly.
func resolvePublishSlug(r *bufio.Reader, interactive bool, src *publishSource) (string, error) {
	if publishSlug != "" {
		return normalizeSlug(publishSlug), nil
	}
	if src.name != "" {
		return normalizeSlug(src.name), nil
	}

	base := filepath.Base(src.path)
	stem := strings.TrimSuffix(base, filepath.Ext(base))

	switch {
	case base == "starship.toml":
		if !interactive {
			return "", noAnswerError("--slug <slug>")
		}
		dirGuess := normalizeSlug(filepath.Base(filepath.Dir(src.path)))
		answer, err := promptText(r, "Slug to publish this theme under", dirGuess, "--slug <slug>")
		if err != nil {
			return "", err
		}
		return normalizeSlug(answer), nil

	case versionFilenameRe.MatchString(stem):
		// A versioned config outside the stellar cache - keep the
		// "theme folder" convention: its own name is just a version number.
		candidate := normalizeSlug(filepath.Base(filepath.Dir(src.path)))
		if candidate == "" {
			return "", fmt.Errorf("couldn't derive a slug from %s; pass --slug", src.path)
		}
		return candidate, nil

	default:
		candidate := normalizeSlug(stem)
		if candidate == "" {
			return "", fmt.Errorf("couldn't derive a slug from %s; pass --slug", src.path)
		}
		return candidate, nil
	}
}

// publishSourceField builds the contract's "source" field: "author/name@ver"
// for a theme already inside StellarHome, otherwise just the file's base
// name.
func publishSourceField(src *publishSource) string {
	switch {
	case src.author != "" && src.name != "" && src.version != "":
		return fmt.Sprintf("%s/%s@%s", src.author, src.name, src.version)
	case src.author != "" && src.name != "":
		return fmt.Sprintf("%s/%s", src.author, src.name)
	default:
		return filepath.Base(src.path)
	}
}

// normalizeVersionFlag strips an optional leading "v", matching how the hub
// (and theme.ParseIdentifier) treat version strings.
func normalizeVersionFlag(v string) string {
	return strings.TrimPrefix(strings.TrimSpace(v), "v")
}

// maxThemeVersion returns the numeric maximum of versions, per the
// contract's definition of "latest" ("the numeric maximum", not whichever
// entry the hub happens to list first - GET /api/{author}/{slug} does order
// by createdAt desc in practice, but relying on that would silently break if
// a version were ever republished out of order).
func maxThemeVersion(versions []api.VersionInfo) string {
	best := ""
	for _, v := range versions {
		if best == "" || theme.CompareSemver(v.Version, best) > 0 {
			best = v.Version
		}
	}
	return best
}

// suggestNextVersion returns a sensible default for the version prompt when
// a version draft is missing one: the next major after the latest published
// version, e.g. "1.2" -> "2.0" - the same default the hub gives a new
// version draft (and what the docs promise: "1.4 becomes 2.0"). Returns "" for
// anything it can't parse as "X.Y" (including an empty latest - a draft for
// a theme with no versions yet, or a hub that didn't send one), which
// promptText then treats as "no default" rather than offering a bogus one.
func suggestNextVersion(latest string) string {
	major, minor, ok := strings.Cut(latest, ".")
	if !ok {
		return ""
	}
	if _, err := strconv.Atoi(minor); err != nil {
		return ""
	}
	n, err := strconv.Atoi(major)
	if err != nil {
		return ""
	}
	return fmt.Sprintf("%d.0", n+1)
}

// versionFloor is the number a new version of the draft's theme has to beat:
// HighestVersion - the latest, or a deleted version above it, since the hub
// never reuses a deleted number - and LatestVersion from an older hub that
// doesn't send HighestVersion.
func versionFloor(th *api.DraftTheme) string {
	if th.HighestVersion != "" {
		return th.HighestVersion
	}
	return th.LatestVersion
}

// titleCase turns a slug ("my-cool-theme") into a readable default display
// name ("My Cool Theme").
func titleCase(slug string) string {
	parts := strings.FieldsFunc(slug, func(r rune) bool { return r == '-' || r == '_' })
	for i, p := range parts {
		if p == "" {
			continue
		}
		parts[i] = strings.ToUpper(p[:1]) + p[1:]
	}
	if len(parts) == 0 {
		return slug
	}
	return strings.Join(parts, " ")
}

// --- draft resolution --------------------------------------------------------

// anyThemeOnlyFlagsGiven reports whether the user passed any flag that only
// makes sense for a theme (as opposed to a version draft of an existing
// theme) - used to warn, once, when one of these is about to be silently
// ignored on a version draft.
func anyThemeOnlyFlagsGiven() bool {
	return publishName != "" || publishDescription != "" || publishGroup != "" || publishColorMode != ""
}

// warnThemeOnlyFlagsIgnored tells the user exactly which flags don't apply
// to a version draft, and where to change that metadata instead.
func warnThemeOnlyFlagsIgnored() {
	base := paths.APIURL(api.BaseURL)
	color.Yellow("--name, --description, --group and --color-mode only apply to new themes - change those in settings: %s/settings", base)
}

// applyExplicitFlagsCreateShared sets the fields that apply to a freshly
// created draft of EITHER kind - a version draft has its own dependencies,
// minStarshipVersion, version and notes just as much as a theme draft does.
func applyExplicitFlagsCreateShared(req *api.CreateDraftRequest) {
	if len(publishDeps) > 0 {
		v := append([]string(nil), publishDeps...)
		req.Dependencies = &v
	}
	if publishMinStarship != "" {
		v := publishMinStarship
		req.MinStarshipVersion = &v
	}
	if publishNotes != "" {
		v := publishNotes
		req.VersionNotes = &v
	}
}

// applyExplicitFlagsCreateThemeOnly sets the fields that only make sense on
// a brand-new theme draft - never call this for a version draft (see
// anyThemeOnlyFlagsGiven/warnThemeOnlyFlagsIgnored instead).
func applyExplicitFlagsCreateThemeOnly(req *api.CreateDraftRequest) {
	if publishDescription != "" {
		v := publishDescription
		req.Description = &v
	}
	if publishGroup != "" {
		v := publishGroup
		req.Group = &v
	}
	if publishColorMode != "" {
		v := publishColorMode
		req.ColorMode = &v
	}
}

// applyExplicitFlagsPatchShared is applyExplicitFlagsCreateShared for PATCH,
// safe to send regardless of the draft's kind.
func applyExplicitFlagsPatchShared(req *api.PatchDraftRequest) {
	if len(publishDeps) > 0 {
		v := append([]string(nil), publishDeps...)
		req.Dependencies = &v
	}
	if publishMinStarship != "" {
		v := publishMinStarship
		req.MinStarshipVersion = &v
	}
	if publishNotes != "" {
		v := publishNotes
		req.VersionNotes = &v
	}
	if publishVersion != "" {
		v := normalizeVersionFlag(publishVersion)
		req.Version = &v
	}
}

// applyExplicitFlagsPatchThemeOnly is applyExplicitFlagsCreateThemeOnly for
// PATCH - only ever called when attaching to a THEME draft. Slug lives here
// too: the hub never accepts it on a version draft at all, and even on a
// theme draft it's only ever sent when the user explicitly passed --slug.
func applyExplicitFlagsPatchThemeOnly(req *api.PatchDraftRequest) {
	if publishDescription != "" {
		v := publishDescription
		req.Description = &v
	}
	if publishGroup != "" {
		v := publishGroup
		req.Group = &v
	}
	if publishColorMode != "" {
		v := publishColorMode
		req.ColorMode = &v
	}
	if publishName != "" {
		v := publishName
		req.Name = &v
	}
	if publishSlug != "" {
		v := normalizeSlug(publishSlug)
		req.Slug = &v
	}
}

func findSlugMatchingDraft(drafts []api.Draft, slug string) *api.Draft {
	for i := range drafts {
		d := &drafts[i]
		if d.Kind == api.DraftKindTheme && d.Slug != nil && *d.Slug == slug {
			return d
		}
		if d.Kind == api.DraftKindVersion && d.Theme != nil && d.Theme.Slug == slug {
			return d
		}
	}
	return nil
}

// findWaitingDrafts returns the user's THEME drafts (only) that are still
// waiting for a config - version drafts are deliberately excluded, so a
// config meant for one theme can never end up attached to some other
// theme's version draft just because both happened to be waiting.
func findWaitingDrafts(drafts []api.Draft) []api.Draft {
	var out []api.Draft
	for _, d := range drafts {
		if d.Kind == api.DraftKindTheme && d.Status == api.DraftStatusWaitingForConfig {
			out = append(out, d)
		}
	}
	return out
}

func derefOr(p *string, fallback string) string {
	if p != nil && *p != "" {
		return *p
	}
	return fallback
}

// relativeOrDate renders an ISO-8601 UTC timestamp as a short relative
// phrase ("5 min ago") for anything recent, falling back to a European
// absolute date/time for anything older than a week.
func relativeOrDate(iso string) string {
	t, err := time.Parse(time.RFC3339, iso)
	if err != nil {
		return iso
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%d min ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%d h ago", int(d.Hours()))
	case d < 7*24*time.Hour:
		return fmt.Sprintf("%d d ago", int(d.Hours()/24))
	default:
		return t.Local().Format("02.01.2006 15:04")
	}
}

// statusAwarePrompt builds the confirmation text for attaching to a draft
// that already matches the target slug, wording it according to where its
// current config came from.
func statusAwarePrompt(d *api.Draft) string {
	code := term.SanitizeForTerminal(d.Code)
	switch {
	case d.ConfigSource == nil, *d.ConfigSource == api.ConfigSourceLatest:
		return fmt.Sprintf("Draft %s is waiting for a config - attach yours?", code)
	case *d.ConfigSource == api.ConfigSourceWeb:
		return fmt.Sprintf("Replace the config you set on the web (%s) with this one?", relativeOrDate(derefOr(d.ConfigUpdatedAt, d.UpdatedAt)))
	case *d.ConfigSource == api.ConfigSourceCLI:
		return fmt.Sprintf("Replace the config you pushed from the CLI (%s) with this one?", relativeOrDate(derefOr(d.ConfigUpdatedAt, d.UpdatedAt)))
	default:
		return fmt.Sprintf("Attach your config to draft %s?", code)
	}
}

// isWaitingForConfig reports whether d's current config would be replaced
// (something actually changes) or whether it's genuinely still waiting
// (nothing gets replaced) - used to pick confirmAttachToDraft's default.
func isWaitingForConfig(d *api.Draft) bool {
	return d.ConfigSource == nil || *d.ConfigSource == api.ConfigSourceLatest
}

// confirmAttachToDraft asks before replacing an existing draft's config.
// Like promptConfirmation elsewhere in this package, it always attempts the
// prompt - a piped real answer still works; only a genuinely missing answer
// (noAnswerError, from promptYesNo) turns into an error naming --yes.
//
// The default is Yes when the draft is only waiting for a config (nothing
// gets replaced by attaching), and No when it would actually overwrite a
// config someone already set, on the web or from the CLI.
func confirmAttachToDraft(r *bufio.Reader, tty bool, d *api.Draft) (bool, error) {
	if publishYes {
		return true, nil
	}
	return promptYesNo(r, tty, statusAwarePrompt(d), isWaitingForConfig(d), "--yes")
}

func waitingDraftLabel(d api.Draft) string {
	name := "(untitled)"
	if d.Name != nil && *d.Name != "" {
		name = term.SanitizeForTerminal(*d.Name)
	}
	return fmt.Sprintf("%s - theme draft %q", term.SanitizeForTerminal(d.Code), name)
}

// promptWaitingDraftChoice offers to attach to one of the user's other
// theme drafts that are still waiting for a config, defaulting to "create a
// new draft" - used only when nothing already matches the target slug.
func promptWaitingDraftChoice(r *bufio.Reader, waiting []api.Draft) (chosen *api.Draft, createNew bool, err error) {
	labels := make([]string, 0, len(waiting)+1)
	for _, d := range waiting {
		labels = append(labels, waitingDraftLabel(d))
	}
	labels = append(labels, "Create a new draft")

	idx, serr := promptSelect(r, "You have other drafts waiting for a config - use one of these instead?", labels, len(labels)-1, "--yes")
	if serr != nil {
		return nil, false, serr
	}
	if idx == len(labels)-1 {
		return nil, true, nil
	}
	return &waiting[idx], false, nil
}

// checkUnchanged confirms before keeping a version draft whose config and
// dependencies are identical to the latest published version - there is
// nothing to publish otherwise, so this is the one place publish asks "are
// you sure" even right after creating a draft.
//
// created distinguishes a draft this very call just made (declining it
// deletes it again, best-effort, so a false start doesn't linger as a dead
// draft blocking the next attempt) from one we merely attached to via PATCH
// (declining leaves it exactly as it now is - its config was already
// updated, so there is nothing left to roll back).
func checkUnchanged(r *bufio.Reader, tty bool, client *api.Client, d *api.Draft, created bool) (*api.Draft, error) {
	if d.Kind != api.DraftKindVersion || !d.UnchangedFromLatest {
		return d, nil
	}
	latest := "the latest version"
	if d.Theme != nil && d.Theme.LatestVersion != "" {
		latest = term.SanitizeForTerminal(d.Theme.LatestVersion)
	}
	if publishYes {
		return d, nil
	}

	ok, err := promptYesNo(r, tty, fmt.Sprintf("Nothing changed vs %s - keep this draft anyway?", latest), false, "--yes")
	if err != nil {
		if created {
			_ = client.DeleteDraft(d.Code)
		}
		return nil, err
	}
	if !ok {
		code := term.SanitizeForTerminal(d.Code)
		if created {
			_ = client.DeleteDraft(d.Code)
			return nil, fmt.Errorf("aborted: config unchanged vs %s (draft %s was not kept)", latest, code)
		}
		return nil, fmt.Errorf("aborted: config unchanged vs %s (draft %s was left as it was - its config had already been updated)", latest, code)
	}
	return d, nil
}

// resolveOrCreateDraft implements the publish decision order: an explicit
// --draft wins outright; otherwise a draft already matching the target slug
// is attached to (after a status-aware confirm); otherwise an existing
// theme under the user's own name means a new version draft, and anything
// else means a brand new theme draft - offering to reuse a draft still
// waiting for a config first, unless one was already found by slug.
func resolveOrCreateDraft(r *bufio.Reader, interactive bool, client *api.Client, me *api.WhoamiResponse, slug string, src *publishSource, content string) (*api.Draft, error) {
	source := publishSourceField(src)

	// attach PATCHes an existing draft of the given kind - which must
	// already be known, since a version draft never accepts the theme-only
	// fields (name/slug/description/group/colorMode) a theme draft does.
	attach := func(code, kind string) (*api.Draft, error) {
		req := api.PatchDraftRequest{Config: &content, Source: &source}
		applyExplicitFlagsPatchShared(&req)
		if kind == api.DraftKindTheme {
			applyExplicitFlagsPatchThemeOnly(&req)
		} else if anyThemeOnlyFlagsGiven() {
			warnThemeOnlyFlagsIgnored()
		}
		d, err := client.PatchDraft(code, req)
		if err != nil {
			return nil, wrapHubAPIError(err)
		}
		return d, nil
	}

	if publishDraftCode != "" {
		code := strings.ToUpper(publishDraftCode)
		existing, gerr := client.GetDraft(code, false)
		if gerr != nil {
			return nil, wrapHubAPIError(gerr)
		}
		d, err := attach(code, existing.Kind)
		if err != nil {
			return nil, err
		}
		return checkUnchanged(r, interactive, client, d, false)
	}

	drafts, err := client.ListDrafts()
	if err != nil {
		return nil, wrapHubAPIError(err)
	}

	if match := findSlugMatchingDraft(drafts, slug); match != nil {
		ok, cerr := confirmAttachToDraft(r, interactive, match)
		if cerr != nil {
			return nil, cerr
		}
		if !ok {
			return nil, fmt.Errorf("aborted: draft %s was not changed", term.SanitizeForTerminal(match.Code))
		}
		d, err := attach(match.Code, match.Kind)
		if err != nil {
			return nil, err
		}
		return checkUnchanged(r, interactive, client, d, false)
	}

	existingTheme, infoErr := api.NewClient().GetThemeInfo(me.Username, slug)
	themeExists := infoErr == nil
	if infoErr != nil && !errors.Is(infoErr, api.ErrNotFound) {
		return nil, fmt.Errorf("couldn't check stellar-hub for %s/%s: %w", me.Username, slug, infoErr)
	}

	if waiting := findWaitingDrafts(drafts); len(waiting) > 0 && interactive {
		chosen, createNew, perr := promptWaitingDraftChoice(r, waiting)
		if perr != nil {
			return nil, perr
		}
		if !createNew {
			d, err := attach(chosen.Code, chosen.Kind)
			if err != nil {
				return nil, err
			}
			return checkUnchanged(r, interactive, client, d, false)
		}
	}

	if themeExists {
		req := api.CreateDraftRequest{Kind: api.DraftKindVersion, ThemeSlug: &slug, Config: &content, Source: &source}
		applyExplicitFlagsCreateShared(&req)
		if anyThemeOnlyFlagsGiven() {
			warnThemeOnlyFlagsIgnored()
		}

		latest := maxThemeVersion(existingTheme.Versions)
		switch {
		case publishVersion != "":
			v := normalizeVersionFlag(publishVersion)
			req.Version = &v
		case src.version != "" && latest != "" && theme.CompareSemver(src.version, latest) > 0:
			v := src.version
			req.Version = &v
		}

		d, cerr := client.CreateDraft(req)
		if cerr != nil {
			return nil, wrapHubAPIError(cerr)
		}
		return checkUnchanged(r, interactive, client, d, true)
	}

	req := api.CreateDraftRequest{Kind: api.DraftKindTheme, Slug: &slug, Config: &content, Source: &source}
	applyExplicitFlagsCreateShared(&req)
	applyExplicitFlagsCreateThemeOnly(&req)

	version := publishVersion
	if version == "" {
		version = src.version
	}
	if version == "" {
		version = "1.0"
	}
	version = normalizeVersionFlag(version)
	req.Version = &version

	name := publishName
	if name == "" {
		// Only asked when interactive. Piped stdin is reserved for the y/N
		// confirmations (`echo y | stellar publish …`), and reading a name
		// from it would swallow that answer as the theme's name; --yes must
		// never prompt at all, so it falls straight to the title-cased
		// default too.
		name = titleCase(slug)
		if interactive {
			name, err = promptText(r, "Theme name", name, "--name <name>")
			if err != nil {
				return nil, err
			}
		}
	}
	req.Name = &name

	d, cerr := client.CreateDraft(req)
	if cerr != nil {
		return nil, wrapHubAPIError(cerr)
	}
	return d, nil
}

// --- output and --publish finalization ---------------------------------------

// draftSavedHint appends a "the draft is still there" hint to err whenever
// a draft-related operation fails after the draft itself already exists
// (a screenshot upload, filling in a missing field, or the final publish
// call) - the draft isn't lost just because finishing it in one command
// didn't work, so the user still needs the link back to it. The hint
// deliberately uses a plain URL (no OSC 8/color escapes) rather than
// term.Hyperlink: it's nested inside the hint-rendering machinery's own
// coloring, and most modern terminals linkify a bare URL on their own
// anyway (see internal/term/hyperlink.go).
func draftSavedHint(err error, d *api.Draft) error {
	if err == nil || d == nil {
		return err
	}
	code := term.SanitizeForTerminal(d.Code)
	return newHintedError(err.Error(), hint{
		label:  "Draft saved",
		values: []string{fmt.Sprintf("%s - finish it on the hub: %s", code, d.URL)},
	})
}

// printDraftSummary prints the "✓ Draft ..." line. author is the logged-in
// username (never trust a theme-draft's own author field for this, since
// one isn't part of the Draft shape at all - the slug alone doesn't say
// whose it is).
func printDraftSummary(d *api.Draft, author string) {
	code := term.SanitizeForTerminal(d.Code)
	author = term.SanitizeForTerminal(author)

	if d.Kind == api.DraftKindVersion {
		slug := ""
		if d.Theme != nil {
			slug = term.SanitizeForTerminal(d.Theme.Slug)
		}
		target := fmt.Sprintf("%s/%s", author, slug)
		if d.Version != nil && *d.Version != "" {
			color.Green("✓ Draft %s: new version %s@%s", code, target, term.SanitizeForTerminal(*d.Version))
		} else {
			// Never print the latest EXISTING version as though it were the
			// new one - with no version chosen yet, there simply isn't one.
			color.Green("✓ Draft %s: new version of %s", code, target)
		}
		fmt.Printf("Finish it on the hub: %s%s\n", term.Hyperlink(d.URL, d.URL), color.HiBlackString(term.ClickHint()))
		return
	}

	name := code
	if d.Name != nil && *d.Name != "" {
		name = term.SanitizeForTerminal(*d.Name)
	}
	if d.Slug != nil && *d.Slug != "" {
		version := "1.0"
		if d.Version != nil && *d.Version != "" {
			version = term.SanitizeForTerminal(*d.Version)
		}
		color.Green("✓ Draft %s: %s (%s/%s@%s)", code, name, author, term.SanitizeForTerminal(*d.Slug), version)
	} else {
		color.Green("✓ Draft %s: %s", code, name)
	}
	fmt.Printf("Finish it on the hub: %s%s\n", term.Hyperlink(d.URL, d.URL), color.HiBlackString(term.ClickHint()))
}

// fillMissingForPublish fills in whatever the draft is still missing before
// --publish can finalize it: an explicit flag is used if given, otherwise an
// interactive prompt - a non-interactive run (no TTY, or --yes) without the
// right flag errors out naming it instead of guessing.
//
// On every error path this still returns the most recent *api.Draft it has
// (never nil) alongside the error, so the caller can always point the user
// back at the draft - which still exists on the hub regardless of why
// filling it in failed - instead of leaving them with nothing but an error.
func fillMissingForPublish(r *bufio.Reader, interactive bool, client *api.Client, d *api.Draft) (*api.Draft, error) {
	if len(d.Missing) == 0 {
		return d, nil
	}

	patch := api.PatchDraftRequest{}
	changed := false

	for _, field := range d.Missing {
		switch field {
		case "name":
			if publishName != "" {
				v := publishName
				patch.Name = &v
				changed = true
				continue
			}
			if !interactive {
				return d, fmt.Errorf("draft %s is missing a name; re-run with --name <name>", term.SanitizeForTerminal(d.Code))
			}
			def := ""
			if d.Slug != nil {
				def = titleCase(*d.Slug)
			}
			name, err := promptText(r, "Theme name", def, "--name <name>")
			if err != nil {
				return d, err
			}
			patch.Name = &name
			changed = true

		case "screenshot":
			if !interactive {
				return d, fmt.Errorf("draft %s is missing a screenshot; re-run with --screenshot <path>", term.SanitizeForTerminal(d.Code))
			}
			updated, serr := promptForScreenshotWithRetries(r, client, d.Code)
			if serr != nil {
				return d, serr
			}
			d = updated

		case "version":
			if publishVersion != "" {
				v := normalizeVersionFlag(publishVersion)
				patch.Version = &v
				changed = true
				continue
			}
			if !interactive {
				return d, fmt.Errorf("draft %s is missing a version; re-run with --version <version>", term.SanitizeForTerminal(d.Code))
			}
			def := ""
			if d.Theme != nil {
				def = suggestNextVersion(versionFloor(d.Theme))
			}
			v, err := promptText(r, "Version", def, "--version <version>")
			if err != nil {
				return d, err
			}
			v = normalizeVersionFlag(v)
			patch.Version = &v
			changed = true

		default:
			// Includes "config", which the hub can report as missing for a
			// version draft whose config is null or whitespace-only - a
			// publish run always sends a non-empty config, so this
			// shouldn't normally happen, but if it does there's nothing
			// sensible for the CLI to fill in on its own.
			return d, fmt.Errorf("draft %s is missing %q, which stellar publish can't fill in - finish it on the hub", term.SanitizeForTerminal(d.Code), term.SanitizeForTerminal(field))
		}
	}

	if changed {
		updated, perr := client.PatchDraft(d.Code, patch)
		if perr != nil {
			return d, wrapHubAPIError(perr)
		}
		d = updated
	}

	if len(d.Missing) > 0 {
		missing := make([]string, len(d.Missing))
		for i, m := range d.Missing {
			missing[i] = term.SanitizeForTerminal(m)
		}
		return d, fmt.Errorf("draft %s is still missing: %s", term.SanitizeForTerminal(d.Code), strings.Join(missing, ", "))
	}
	return d, nil
}

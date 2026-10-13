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
	publishCmd.Flags().StringVar(&publishDescription, "description", "", "Theme description")
	publishCmd.Flags().StringVar(&publishGroup, "group", "", "Theme group/category")
	publishCmd.Flags().StringVar(&publishColorMode, "color-mode", "", "dark, light, or both")
	publishCmd.Flags().StringArrayVar(&publishDeps, "dep", nil, "A dependency theme requires (repeatable)")
	publishCmd.Flags().StringVar(&publishMinStarship, "min-starship", "", "Minimum starship version required")
	publishCmd.Flags().StringVar(&publishScreenshot, "screenshot", "", "Path to a screenshot (PNG/JPEG/WebP, max 4MB)")
	publishCmd.Flags().StringVar(&publishVersion, "version", "", "Version to publish (default: from the file, or auto-bumped)")
	publishCmd.Flags().StringVar(&publishNotes, "notes", "", "Version notes")
	publishCmd.Flags().StringVar(&publishDraftCode, "draft", "", "Attach to an existing draft by its code")
	publishCmd.Flags().BoolVar(&publishDoPublish, "publish", false, "Finish and publish immediately instead of leaving a draft")
	publishCmd.Flags().BoolVarP(&publishYes, "yes", "y", false, "Never prompt; assume yes and fail instead of asking")

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

func runPublish(cmd *cobra.Command, args []string) error {
	r := newPromptReader(cmd)
	tty := isTerminal(cmd.InOrStdin())

	src, err := resolvePublishSource(r, tty, args[0])
	if err != nil {
		return err
	}
	fmt.Printf("Using %s\n", src.path)

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
	if src.author != "" && src.author != me.Username {
		themeID := fmt.Sprintf("%s/%s", src.author, src.name)
		if cfgData.HasDownloaded(themeID) {
			return fmt.Errorf("%s is %s's theme - use `stellar fork %s` to make your own editable copy", themeID, src.author, themeID)
		}
	}

	slug, err := resolvePublishSlug(r, tty, src)
	if err != nil {
		return err
	}
	if slug == theme.BackupThemeName {
		return fmt.Errorf("the slug %q is reserved for stellar's own config backups - pick another with --slug", slug)
	}

	client := api.NewAuthedClient(token)
	draft, err := resolveOrCreateDraft(r, tty, client, me, slug, src, content)
	if err != nil {
		return err
	}

	if len(screenshotData) > 0 {
		updated, uerr := client.UploadScreenshot(draft.Code, screenshotData)
		if uerr != nil {
			return wrapHubAPIError(uerr)
		}
		draft = updated
	}

	if !publishDoPublish {
		printDraftSummary(draft)
		return nil
	}

	draft, err = fillMissingForPublish(r, tty, client, draft)
	if err != nil {
		return draftSavedHint(err, draft)
	}

	result, perr := client.PublishDraft(draft.Code)
	if perr != nil {
		return draftSavedHint(wrapHubAPIError(perr), draft)
	}

	color.Green("✓ Published %s/%s@%s", result.Author, result.Slug, result.Version)
	fmt.Printf("  %s%s\n", term.Hyperlink(result.URL, result.URL), color.HiBlackString(term.ClickHint()))
	return nil
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
func resolvePublishSource(r *bufio.Reader, tty bool, arg string) (*publishSource, error) {
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
		return nil, fmt.Errorf("%q is not a path, a cached author/slug, or a theme name under your stellar cache", arg)
	}

	chosen := matches[0]
	if len(matches) > 1 {
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

// resolveExistingPath reports whether arg is (or resolves, via symlinks, to)
// an existing file or directory. isPath is true whenever arg looks like a
// path attempt at all (it exists on disk) - a directory with nothing usable
// in it is still isPath=true, with err set, since nothing else in the
// precedence order would make sense of it either.
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

	if ver, verr := theme.FindLatestLocalVersion(resolved); verr == nil {
		return filepath.Join(resolved, ver+".toml"), true, nil
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

	if home, herr := paths.StellarHome(); herr == nil {
		if resolvedHome, everr := filepath.EvalSymlinks(home); everr == nil {
			home = resolvedHome
		}
		if rel, relErr := filepath.Rel(home, abs); relErr == nil && !strings.HasPrefix(rel, "..") && rel != "." {
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
		return "", theme.ValidationResult{}, fmt.Errorf("invalid config: %w", result.Error)
	}

	if n := utf16Length(content); n == 0 || n > maxConfigUTF16Units {
		return "", theme.ValidationResult{}, fmt.Errorf("config must be 1-%d UTF-16 code units (got %d)", maxConfigUTF16Units, n)
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
	if api.DetectImageContentType(data) == "" {
		return nil, fmt.Errorf("%s doesn't look like a PNG, JPEG or WebP image", path)
	}
	if len(data) > api.MaxScreenshotBytes {
		return nil, fmt.Errorf("%s is too large (max %d bytes)", path, api.MaxScreenshotBytes)
	}
	return data, nil
}

// --- identity ----------------------------------------------------------------

// publishWhoami resolves the logged-in user, offering an inline login when
// stdin is a terminal and nothing (valid) is stored - rather than making the
// user run `stellar auth` themselves and retype the publish command.
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

	if !isTerminal(cmd.InOrStdin()) {
		return nil, "", notLoggedInError()
	}

	ok, perr := promptYesNo(r, "You're not logged in to stellar-hub. Log in now?", true, true, "stellar auth")
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
// its directory name (normalized); a loose starship.toml (no stellar-managed
// directory to name it after) is asked for explicitly instead of guessing
// from whatever directory it happens to sit in.
func resolvePublishSlug(r *bufio.Reader, tty bool, src *publishSource) (string, error) {
	if publishSlug != "" {
		return normalizeSlug(publishSlug), nil
	}
	if src.name != "" {
		return normalizeSlug(src.name), nil
	}

	if filepath.Base(src.path) == "starship.toml" {
		if !tty {
			return "", noAnswerError("--slug <slug>")
		}
		dirGuess := normalizeSlug(filepath.Base(filepath.Dir(src.path)))
		answer, err := promptText(r, "Slug to publish this theme under", dirGuess, "--slug <slug>")
		if err != nil {
			return "", err
		}
		return normalizeSlug(answer), nil
	}

	candidate := normalizeSlug(filepath.Base(filepath.Dir(src.path)))
	if candidate == "" {
		return "", fmt.Errorf("couldn't derive a slug from %s; pass --slug", src.path)
	}
	return candidate, nil
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

func applyExplicitFlagsCreate(req *api.CreateDraftRequest) {
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

func applyExplicitFlagsPatch(req *api.PatchDraftRequest) {
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
	if publishName != "" {
		v := publishName
		req.Name = &v
	}
	if publishVersion != "" {
		v := normalizeVersionFlag(publishVersion)
		req.Version = &v
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

func findWaitingDrafts(drafts []api.Draft) []api.Draft {
	var out []api.Draft
	for _, d := range drafts {
		if d.Status == api.DraftStatusWaitingForConfig {
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
	switch {
	case d.ConfigSource == nil, *d.ConfigSource == api.ConfigSourceLatest:
		return fmt.Sprintf("Draft %s is waiting for a config - attach yours?", d.Code)
	case *d.ConfigSource == api.ConfigSourceWeb:
		return fmt.Sprintf("Replace the config you set on the web (%s) with this one?", relativeOrDate(derefOr(d.ConfigUpdatedAt, d.UpdatedAt)))
	case *d.ConfigSource == api.ConfigSourceCLI:
		return fmt.Sprintf("Replace the config you pushed from the CLI (%s) with this one?", relativeOrDate(derefOr(d.ConfigUpdatedAt, d.UpdatedAt)))
	default:
		return fmt.Sprintf("Attach your config to draft %s?", d.Code)
	}
}

// confirmAttachToDraft asks before replacing an existing draft's config. Like
// promptConfirmation elsewhere in this package, it always attempts the
// prompt regardless of whether stdin looks like a terminal - a piped real
// answer (`echo y | stellar publish ...`) still works; only a genuinely
// missing answer (noAnswerError, from promptYesNo) turns into an error
// naming --yes.
func confirmAttachToDraft(r *bufio.Reader, d *api.Draft) (bool, error) {
	if publishYes {
		return true, nil
	}
	return promptYesNo(r, statusAwarePrompt(d), false, false, "--yes")
}

func waitingDraftLabel(d api.Draft) string {
	switch {
	case d.Kind == api.DraftKindTheme && d.Name != nil && *d.Name != "":
		return fmt.Sprintf("%s - theme draft %q", d.Code, term.SanitizeForTerminal(*d.Name))
	case d.Kind == api.DraftKindVersion && d.Theme != nil:
		return fmt.Sprintf("%s - new version of %s", d.Code, term.SanitizeForTerminal(d.Theme.Name))
	default:
		return d.Code
	}
}

// promptWaitingDraftChoice offers to attach to one of the user's other
// drafts that are still waiting for a config, defaulting to "create a new
// draft" - used only when nothing already matches the target slug.
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
func checkUnchanged(r *bufio.Reader, client *api.Client, d *api.Draft, created bool) (*api.Draft, error) {
	if d.Kind != api.DraftKindVersion || !d.UnchangedFromLatest {
		return d, nil
	}
	latest := "the latest version"
	if d.Theme != nil && d.Theme.LatestVersion != "" {
		latest = d.Theme.LatestVersion
	}
	if publishYes {
		return d, nil
	}

	ok, err := promptYesNo(r, fmt.Sprintf("Nothing changed vs %s - keep this draft anyway?", latest), false, false, "--yes")
	if err != nil {
		if created {
			_ = client.DeleteDraft(d.Code)
		}
		return nil, err
	}
	if !ok {
		if created {
			_ = client.DeleteDraft(d.Code)
			return nil, fmt.Errorf("aborted: config unchanged vs %s (draft %s was not kept)", latest, d.Code)
		}
		return nil, fmt.Errorf("aborted: config unchanged vs %s (draft %s was left as it was - its config had already been updated)", latest, d.Code)
	}
	return d, nil
}

// resolveOrCreateDraft implements the publish decision order: an explicit
// --draft wins outright; otherwise a draft already matching the target slug
// is attached to (after a status-aware confirm); otherwise an existing
// theme under the user's own name means a new version draft, and anything
// else means a brand new theme draft - offering to reuse a draft still
// waiting for a config first, unless one was already found by slug.
func resolveOrCreateDraft(r *bufio.Reader, tty bool, client *api.Client, me *api.WhoamiResponse, slug string, src *publishSource, content string) (*api.Draft, error) {
	source := publishSourceField(src)

	attach := func(code string) (*api.Draft, error) {
		req := api.PatchDraftRequest{Config: &content, Source: &source}
		applyExplicitFlagsPatch(&req)
		d, err := client.PatchDraft(code, req)
		if err != nil {
			return nil, wrapHubAPIError(err)
		}
		return d, nil
	}

	if publishDraftCode != "" {
		d, err := attach(publishDraftCode)
		if err != nil {
			return nil, err
		}
		return checkUnchanged(r, client, d, false)
	}

	drafts, err := client.ListDrafts()
	if err != nil {
		return nil, wrapHubAPIError(err)
	}

	if match := findSlugMatchingDraft(drafts, slug); match != nil {
		ok, cerr := confirmAttachToDraft(r, match)
		if cerr != nil {
			return nil, cerr
		}
		if !ok {
			return nil, fmt.Errorf("aborted: draft %s was not changed", match.Code)
		}
		d, err := attach(match.Code)
		if err != nil {
			return nil, err
		}
		return checkUnchanged(r, client, d, false)
	}

	existing, infoErr := api.NewClient().GetThemeInfo(me.Username, slug)
	themeExists := infoErr == nil
	if infoErr != nil && !errors.Is(infoErr, api.ErrNotFound) {
		return nil, fmt.Errorf("couldn't check stellar-hub for %s/%s: %w", me.Username, slug, infoErr)
	}

	if waiting := findWaitingDrafts(drafts); len(waiting) > 0 && tty && !publishYes {
		chosen, createNew, perr := promptWaitingDraftChoice(r, waiting)
		if perr != nil {
			return nil, perr
		}
		if !createNew {
			d, err := attach(chosen.Code)
			if err != nil {
				return nil, err
			}
			return checkUnchanged(r, client, d, false)
		}
	}

	if themeExists {
		req := api.CreateDraftRequest{Kind: api.DraftKindVersion, ThemeSlug: &slug, Config: &content, Source: &source}
		applyExplicitFlagsCreate(&req)

		latest := maxThemeVersion(existing.Versions)
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
		return checkUnchanged(r, client, d, true)
	}

	req := api.CreateDraftRequest{Kind: api.DraftKindTheme, Slug: &slug, Config: &content, Source: &source}
	applyExplicitFlagsCreate(&req)

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
		// Only asked on a terminal. Piped stdin is reserved for the y/N
		// confirmations (`echo y | stellar publish …`), and reading a name from
		// it would swallow that answer as the theme's name.
		name = titleCase(slug)
		if tty {
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
// --publish fails after the draft itself already exists (filling in a
// missing field, or the final publish call) - the draft isn't lost just
// because finishing it in one command didn't work, so the user still needs
// the link back to it. The hint deliberately uses a plain URL (no OSC 8/
// color escapes) rather than term.Hyperlink: it's nested inside the
// hint-rendering machinery's own coloring, and most modern terminals
// linkify a bare URL on their own anyway (see internal/term/hyperlink.go).
func draftSavedHint(err error, d *api.Draft) error {
	if err == nil || d == nil {
		return err
	}
	return newHintedError(err.Error(), hint{
		label:  "Draft saved",
		values: []string{fmt.Sprintf("%s - finish it on the hub: %s", d.Code, d.URL)},
	})
}

func printDraftSummary(d *api.Draft) {
	name := d.Code
	switch {
	case d.Name != nil && *d.Name != "":
		name = term.SanitizeForTerminal(*d.Name)
	case d.Theme != nil:
		name = term.SanitizeForTerminal(d.Theme.Name)
	}

	version := "1.0"
	if d.Version != nil && *d.Version != "" {
		version = *d.Version
	} else if d.Theme != nil && d.Theme.LatestVersion != "" {
		version = d.Theme.LatestVersion
	}

	target := ""
	switch {
	case d.Slug != nil:
		target = *d.Slug
	case d.Theme != nil:
		target = d.Theme.Slug
	}

	switch {
	case d.Kind == api.DraftKindVersion && target != "":
		color.Green("✓ Draft %s: new version %s of %s", d.Code, version, name)
	case target != "":
		color.Green("✓ Draft %s: %s (%s %s)", d.Code, name, target, version)
	default:
		color.Green("✓ Draft %s: %s", d.Code, name)
	}
	fmt.Printf("Finish it on the hub: %s%s\n", term.Hyperlink(d.URL, d.URL), color.HiBlackString(term.ClickHint()))
}

// fillMissingForPublish fills in whatever the draft is still missing before
// --publish can finalize it: an explicit flag is used if given, otherwise an
// interactive prompt (TTY only) - a non-interactive run without the right
// flag errors out naming it instead of guessing.
//
// On every error path this still returns the most recent *api.Draft it has
// (never nil) alongside the error, so the caller can always point the user
// back at the draft - which still exists on the hub regardless of why
// filling it in failed - instead of leaving them with nothing but an error.
func fillMissingForPublish(r *bufio.Reader, tty bool, client *api.Client, d *api.Draft) (*api.Draft, error) {
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
			if !tty {
				return d, fmt.Errorf("draft %s is missing a name; re-run with --name <name>", d.Code)
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
			if !tty {
				return d, fmt.Errorf("draft %s is missing a screenshot; re-run with --screenshot <path>", d.Code)
			}
			path, err := promptText(r, "Path to a screenshot (PNG/JPEG/WebP, max 4MB)", "", "--screenshot <path>")
			if err != nil {
				return d, err
			}
			data, lerr := loadScreenshot(path)
			if lerr != nil {
				return d, lerr
			}
			updated, uerr := client.UploadScreenshot(d.Code, data)
			if uerr != nil {
				return d, wrapHubAPIError(uerr)
			}
			d = updated

		case "version":
			if publishVersion != "" {
				v := normalizeVersionFlag(publishVersion)
				patch.Version = &v
				changed = true
				continue
			}
			if !tty {
				return d, fmt.Errorf("draft %s is missing a version; re-run with --version <version>", d.Code)
			}
			v, err := promptText(r, "Version", "", "--version <version>")
			if err != nil {
				return d, err
			}
			v = normalizeVersionFlag(v)
			patch.Version = &v
			changed = true

		default:
			return d, fmt.Errorf("draft %s is missing %q, which stellar publish can't fill in - finish it on the hub", d.Code, field)
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
		return d, fmt.Errorf("draft %s is still missing: %s", d.Code, strings.Join(d.Missing, ", "))
	}
	return d, nil
}

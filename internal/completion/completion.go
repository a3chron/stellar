// Package completion implements shell tab-completion for stellar theme
// identifiers ("author/slug@version"), shared by the commands that accept
// one (apply, preview, info) or several (remove) of them.
package completion

import (
	"fmt"
	"strings"

	"github.com/a3chron/stellar/internal/api"
	"github.com/a3chron/stellar/internal/cache"
	"github.com/a3chron/stellar/internal/theme"
	"github.com/spf13/cobra"
)

// Mode controls whether ThemeIdentifier is allowed to reach out to the
// stellar-hub API in addition to the local cache.
type Mode int

const (
	// LocalOnly restricts completion to the local cache (~/.config/stellar).
	// This is the default for every command: remote lookups (even with a 2s
	// cap) make TAB feel broken, and `stellar remove` only ever operates on
	// cached themes anyway.
	LocalOnly Mode = iota
	// LocalAndRemote additionally queries the stellar-hub API when the local
	// cache alone doesn't have enough to complete usefully. Opt-in via the
	// EnvOnline environment variable.
	LocalAndRemote
)

// EnvOnline opts apply/preview/info completion in to hub suggestions
// ("1" or "true"). Off by default: completion must never feel slow.
const EnvOnline = "STELLAR_COMPLETION_ONLINE"

const (
	descLocal = "local"
	descHub   = "hub"
)

// ThemeIdentifier completes an "author/slug@version" identifier in three
// stages, splitting on the first "/" and then the first "@":
//
//   - Stage A (no "/" yet): author names, emitted as "author/".
//   - Stage B ("author/" typed, no "@" yet): slugs for that author, emitted
//     as "author/slug".
//   - Stage C ("@" typed): versions for that author/slug, emitted as
//     "author/slug@version".
//
// It never blocks on the network for long: any remote lookup goes through
// api.NewCompletionClient (2s timeout), and any error from it degrades
// silently to whatever local results were already gathered - logged only via
// cobra.CompDebugln, since stray text on stdout would corrupt what the
// user's shell parses as completion candidates.
//
// It also tolerates a local cache that doesn't exist yet: every cache
// listing helper it calls returns (nil, nil) rather than an error for a
// missing directory, so completion always degrades to "no candidates"
// instead of failing.
func ThemeIdentifier(toComplete string, mode Mode) ([]string, cobra.ShellCompDirective) {
	slashIdx := strings.IndexByte(toComplete, '/')
	if slashIdx == -1 {
		return completeAuthor(toComplete, mode)
	}

	author := toComplete[:slashIdx]
	rest := toComplete[slashIdx+1:]

	// An empty author ("/x") can never form a valid identifier, and letting
	// it through would list the cache root's author dirs as slugs of "".
	if !theme.IsValidSegment(author) {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	atIdx := strings.IndexByte(rest, '@')
	if atIdx == -1 {
		return completeSlug(author, rest, mode)
	}

	slug := rest[:atIdx]
	versionPrefix := rest[atIdx+1:]

	// Same for an empty slug ("author/@"): nothing valid can complete it.
	if !theme.IsValidSegment(slug) {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	return completeVersion(author, slug, versionPrefix, mode)
}

// completeAuthor implements Stage A: local author directories first; if (and
// only if) toComplete is non-empty and none of them match, fall back to
// querying the hub for authors by prefix.
func completeAuthor(toComplete string, mode Mode) ([]string, cobra.ShellCompDirective) {
	directive := cobra.ShellCompDirectiveNoFileComp | cobra.ShellCompDirectiveNoSpace

	if !validSegment(toComplete) {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	authors, _ := cache.ListAuthors()

	var candidates []string
	for _, a := range authors {
		// The cache is a plain directory: it can hold anything a synced
		// dotfiles checkout or an extracted tarball put there, so its entries
		// get the same grammar check as the hub's (see below).
		if !theme.IsValidSegment(a) {
			continue
		}
		if strings.HasPrefix(a, toComplete) {
			candidates = append(candidates, withDesc(a+"/", descLocal))
		}
	}

	// Empty input never triggers a network call: it's a legitimate case
	// (user just typed the bare command) and local-only is a good enough
	// answer. Same if a local author already matched, or the caller is
	// LocalOnly.
	if toComplete == "" || len(candidates) > 0 || mode == LocalOnly {
		return candidates, directive
	}

	client := api.NewCompletionClient()
	summaries, err := client.SearchThemesByAuthorName(toComplete)
	if err != nil {
		cobra.CompDebugln(err.Error(), false)
		return candidates, directive
	}

	seen := make(map[string]bool)
	for _, s := range summaries {
		// The hub response is untrusted input: never emit a name that
		// couldn't have come from ParseIdentifier's character class, or a
		// hostile hub could inject control sequences into the user's shell.
		if !theme.IsValidSegment(s.Author.Name) {
			continue
		}
		// The hub matches authorName case-insensitively, but every shell
		// filters candidates against the typed word itself - bash's compgen
		// and zsh's compadd case-sensitively. A candidate that doesn't
		// prefix-match what the user typed is silently dropped there, so
		// emitting one would make TAB work in fish and PowerShell only.
		// Filtering here keeps behaviour identical across shells (and doesn't
		// rely on the server having honoured authorName at all).
		if !strings.HasPrefix(s.Author.Name, toComplete) {
			continue
		}
		if seen[s.Author.Name] {
			continue
		}
		seen[s.Author.Name] = true
		candidates = append(candidates, withDesc(s.Author.Name+"/", descHub))
	}

	return candidates, directive
}

// completeSlug implements Stage B: local slugs for author first, then (in
// LocalAndRemote mode) that author's hub themes appended, deduplicated on
// slug. Every candidate keeps the author exactly as the user typed it: the
// hub's /api/{author}/{slug} routes resolve authors with an exact match, so a
// hub theme is only suggested when the typed casing is the hub's casing -
// otherwise the completion would either 404 on apply or (see completeAuthor)
// be dropped by the shell's own prefix filter anyway.
func completeSlug(author, slugPrefix string, mode Mode) ([]string, cobra.ShellCompDirective) {
	directive := cobra.ShellCompDirectiveNoFileComp | cobra.ShellCompDirectiveKeepOrder

	if !validSegment(slugPrefix) {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	localSlugs, _ := cache.ListAuthorThemes(author)

	seen := make(map[string]bool)
	var candidates []string
	for _, slug := range localSlugs {
		// Cache entries get the same grammar check as hub ones - a stray
		// directory must not become a suggestion (see completeAuthor).
		if !theme.IsValidSegment(slug) {
			continue
		}
		if !strings.HasPrefix(slug, slugPrefix) {
			continue
		}
		seen[slug] = true
		candidates = append(candidates, withDesc(author+"/"+slug, descLocal))
	}

	if mode == LocalOnly {
		return candidates, directive
	}

	client := api.NewCompletionClient()
	summaries, err := client.SearchThemesByAuthorName(author)
	if err != nil {
		cobra.CompDebugln(err.Error(), false)
		return candidates, directive
	}

	for _, s := range summaries {
		// Exact, not EqualFold: the hub resolves authors exactly, and the
		// user's shell filters candidates against the typed word.
		if s.Author.Name != author {
			continue
		}
		// Untrusted hub response: only emit values matching the identifier
		// character class (see completeAuthor).
		if !theme.IsValidSegment(s.Slug) {
			continue
		}
		if seen[s.Slug] || !strings.HasPrefix(s.Slug, slugPrefix) {
			continue
		}
		seen[s.Slug] = true
		candidates = append(candidates, withDesc(s.Author.Name+"/"+s.Slug, descHub))
	}

	return candidates, directive
}

// completeVersion implements Stage C: local versions newest-first, then (in
// LocalAndRemote mode) remote versions from GetThemeInfo not already
// present, then the "latest" keyword last. Remote is skipped entirely for
// the reserved backup theme (theme.BackupThemeName): it's never published on
// the hub, so looking it up there would only cost a doomed round trip.
func completeVersion(author, slug, versionPrefix string, mode Mode) ([]string, cobra.ShellCompDirective) {
	directive := cobra.ShellCompDirectiveNoFileComp | cobra.ShellCompDirectiveKeepOrder

	if !validVersionSegment(versionPrefix) {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	// The parser accepts an optional "v" before the version ("@v1.0"), but
	// versions are stored without it. Strip it for matching and re-prepend
	// it on emission so the shell's own prefix filter keeps the candidates.
	// No prefix of "latest" starts with "v", so a leading "v" is unambiguous.
	emitV := strings.HasPrefix(versionPrefix, "v")
	if emitV {
		versionPrefix = versionPrefix[1:]
	}
	emit := func(version string) string {
		if emitV {
			version = "v" + version
		}
		return identifierAt(author, slug, version)
	}
	// suggestable gates every candidate, local or remote, on the grammar
	// apply will later hold it to: "@1.0.1" or "@notes" would only complete
	// to an "invalid theme identifier" error. "@vlatest" parses but is silly.
	suggestable := func(version string) bool {
		if !theme.IsValidVersion(version) {
			return false
		}
		if emitV && version == "latest" {
			return false
		}
		return strings.HasPrefix(version, versionPrefix)
	}

	localVersions, _ := cache.ListThemeVersions(author, slug) // already newest-first

	seen := make(map[string]bool)
	var candidates []string
	for _, v := range localVersions {
		// A theme directory can hold any *.toml name ("1.0.1.toml",
		// "notes.toml", an editor backup), and ListThemeVersions reports the
		// filename verbatim - hence the same gate the hub values get.
		if !suggestable(v) {
			continue
		}
		seen[v] = true
		candidates = append(candidates, withDesc(emit(v), descLocal))
	}

	if mode == LocalAndRemote && slug != theme.BackupThemeName {
		client := api.NewCompletionClient()
		info, err := client.GetThemeInfo(author, slug)
		if err != nil {
			cobra.CompDebugln(err.Error(), false)
		} else {
			for _, v := range info.Versions {
				// Untrusted hub response (see completeAuthor).
				if !suggestable(v.Version) {
					continue
				}
				if seen[v.Version] {
					continue
				}
				seen[v.Version] = true
				candidates = append(candidates, withDesc(emit(v.Version), descHub))
			}
		}
	}

	if !seen["latest"] && suggestable("latest") {
		candidates = append(candidates, identifierAt(author, slug, "latest"))
	}

	return candidates, directive
}

func identifierAt(author, slug, version string) string {
	return fmt.Sprintf("%s/%s@%s", author, slug, version)
}

func withDesc(value, desc string) string {
	return value + "\t" + desc
}

// validSegment reports whether s is a viable *partial* author or slug - i.e.
// what the user has typed so far, so the empty string qualifies. Candidates
// about to be emitted are held to the stricter theme.IsValidSegment instead.
func validSegment(s string) bool {
	for _, r := range s {
		if !theme.IsValidIdentifierRune(r) {
			return false
		}
	}
	return true
}

// validVersionSegment is like validSegment but additionally allows '.', since
// a partially typed version looks like "1", "1." or "lat". Emitted versions
// are held to theme.IsValidVersion instead.
func validVersionSegment(s string) bool {
	for _, r := range s {
		if r != '.' && !theme.IsValidIdentifierRune(r) {
			return false
		}
	}
	return true
}

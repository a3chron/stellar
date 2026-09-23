// cmd/suggest.go builds best-effort "did you mean" suggestions for the
// not-found errors in cmd/hub_errors.go: a closest existing version when a
// theme exists but the requested version doesn't, and closest theme
// identifiers (possibly under a different author) when the theme itself
// doesn't exist.
//
// Every hub call here goes through a short-timeout client
// (api.NewCompletionClient, the same one shell completion uses for its
// opt-in hub lookups) and every error from it is swallowed: a slow or
// unreachable hub must degrade to "no suggestion", never turn a clear
// not-found into a confusing network error, and never block the command for
// long just to embellish an error message that's about to fail it anyway.
package cmd

import (
	"sort"
	"strconv"
	"strings"

	"github.com/a3chron/stellar/internal/api"
	"github.com/a3chron/stellar/internal/cache"
	"github.com/a3chron/stellar/internal/theme"
)

// maxSuggestions bounds how many "did you mean" candidates are ever shown at
// once - past a couple, a list of near-misses reads as noise rather than
// help.
const maxSuggestions = 3

// closestVersion returns the entry of available (any order) that best
// matches requested: the one with the same major version and the nearest
// minor, or - when nothing shares a major - the highest available version.
// Returns "" if available is empty or none of it parses as a version.
func closestVersion(requested string, available []string) string {
	reqMajor, reqMinor, reqOk := parseVersionParts(requested)

	best := ""
	bestDiff := -1
	for _, v := range available {
		major, minor, ok := parseVersionParts(v)
		if !ok {
			continue
		}
		if reqOk && major == reqMajor {
			diff := minor - reqMinor
			if diff < 0 {
				diff = -diff
			}
			if bestDiff == -1 || diff < bestDiff {
				bestDiff = diff
				best = v
			}
		}
	}
	if best != "" {
		return best
	}

	// No shared major (or the requested version isn't parseable at all) -
	// fall back to the highest available version.
	highest := ""
	for _, v := range available {
		if _, _, ok := parseVersionParts(v); !ok {
			continue
		}
		if highest == "" || theme.CompareSemver(v, highest) > 0 {
			highest = v
		}
	}
	return highest
}

// sortVersionsAscending returns a copy of versions sorted oldest-first, for
// display in a not-found error ("Available versions: 1.0, 1.1").
func sortVersionsAscending(versions []string) []string {
	sorted := append([]string(nil), versions...)
	sort.Slice(sorted, func(i, j int) bool {
		return theme.CompareSemver(sorted[i], sorted[j]) < 0
	})
	return sorted
}

// parseVersionParts parses a version string ("1.2") into its major/minor
// components. It's a local, minimal counterpart to theme's own (unexported)
// semver parsing - kept here rather than exported from internal/theme since
// nothing else needs it.
func parseVersionParts(v string) (major, minor int, ok bool) {
	parts := strings.Split(v, ".")
	if len(parts) != 2 {
		return 0, 0, false
	}
	major, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, false
	}
	minor, err = strconv.Atoi(parts[1])
	if err != nil {
		return 0, 0, false
	}
	return major, minor, true
}

// themeCandidate is a possible "did you mean" theme, scored against the
// identifier that wasn't found.
type themeCandidate struct {
	author string
	slug   string
	score  int // lower is better; only meaningful among accepted candidates
}

func (c themeCandidate) identifier() string {
	return c.author + "/" + c.slug
}

// suggestionThreshold is the maximum edit distance that still counts as
// "close enough to suggest": a third of the name, at least 1 and at most 3.
// Theme names share a lot of structure (ctp-blue, ctp-red, ctp-green), so a
// looser rule suggests siblings rather than the one that was mistyped.
func suggestionThreshold(reference string) int {
	return max(1, min(3, len(reference)/3))
}

// closeEnough reports whether candidate is worth suggesting for reference,
// and how good a match it is (lower is better). It accepts a candidate that
// is: case-insensitively equal, within suggestionThreshold edits
// (Damerau-Levenshtein) of reference, or a prefix/substring of the other
// with both sides at least 3 characters (so "a"/"ab" doesn't match anything
// under the sun).
func closeEnough(candidate, reference string) (ok bool, score int) {
	if strings.EqualFold(candidate, reference) {
		return true, 0
	}

	lc, lr := strings.ToLower(candidate), strings.ToLower(reference)
	dist := damerauLevenshtein(lc, lr)
	if dist <= suggestionThreshold(reference) {
		return true, dist
	}

	if len(lc) >= 3 && len(lr) >= 3 && (strings.Contains(lc, lr) || strings.Contains(lr, lc)) {
		return true, dist
	}

	return false, dist
}

// rankThemeCandidates filters candidates down to the ones close enough to
// author/slug to be worth suggesting (by slug alone, or by the full
// "author/slug" identifier - whichever scores better), dedupes, drops the
// identifier that was actually asked for (it's not a suggestion), sorts best
// match first, and caps the result at maxSuggestions.
func rankThemeCandidates(author, slug string, candidates []themeCandidate) []string {
	fullReference := author + "/" + slug

	seen := make(map[string]bool, len(candidates))
	var accepted []themeCandidate
	for _, c := range candidates {
		id := c.identifier()
		if seen[id] || strings.EqualFold(id, fullReference) {
			continue
		}
		seen[id] = true

		// Matched on the slug alone. Comparing the whole author/slug made the
		// shared author count as similarity - "a3chron/ctp-bleu" was within
		// the (length-scaled) limit of "a3chron/ctp-red".
		slugOk, slugScore := closeEnough(c.slug, slug)
		if !slugOk {
			continue
		}
		c.score = slugScore
		// The same slug under another author is still worth suggesting (the
		// author was mistyped), but the asked-for author's own themes rank
		// first.
		if !strings.EqualFold(c.author, author) {
			c.score++
		}
		accepted = append(accepted, c)
	}

	sort.Slice(accepted, func(i, j int) bool {
		if accepted[i].score != accepted[j].score {
			return accepted[i].score < accepted[j].score
		}
		return accepted[i].identifier() < accepted[j].identifier()
	})

	if len(accepted) > maxSuggestions {
		accepted = accepted[:maxSuggestions]
	}

	out := make([]string, len(accepted))
	for i, c := range accepted {
		out[i] = c.identifier()
	}
	return out
}

// themeSuggestions is the best-effort result of looking for themes close to
// an author/slug that doesn't exist on the hub.
type themeSuggestions struct {
	// candidates are ranked "author/slug" identifiers worth suggesting, at
	// most maxSuggestions.
	candidates []string
	// authorHasThemes is true when the hub was successfully asked and
	// reported at least one theme published under exactly this author
	// (case-insensitively). authorChecked is false when that check couldn't
	// be made at all (offline, hub error) - callers must not conclude "no
	// author" from an unchecked result.
	authorHasThemes bool
	authorChecked   bool
}

// gatherThemeSuggestions looks for themes close to author/slug from three
// best-effort sources: the author's own hub themes (also used to tell "no
// such author" from "author exists, wrong slug"), a hub-wide search on the
// slug (catches a right-slug-wrong-author typo), and the local cache. Every
// source degrades silently on error - offline or a slow hub just means
// fewer (or zero) candidates, never a failure.
func gatherThemeSuggestions(author, slug string) themeSuggestions {
	client := api.NewCompletionClient()

	var candidates []themeCandidate
	result := themeSuggestions{}

	if summaries, err := client.SearchThemesByAuthorName(author); err == nil {
		result.authorChecked = true
		for _, s := range summaries {
			if !strings.EqualFold(s.Author.Name, author) {
				continue
			}
			result.authorHasThemes = true
			candidates = append(candidates, themeCandidate{author: s.Author.Name, slug: s.Slug})
		}
	}

	if summaries, err := client.SearchThemes(slug); err == nil {
		for _, s := range summaries {
			candidates = append(candidates, themeCandidate{author: s.Author.Name, slug: s.Slug})
		}
	}

	if cached, err := cache.ListCachedThemes(); err == nil {
		for _, id := range cached {
			ct, perr := theme.ParseIdentifier(id)
			if perr != nil {
				continue
			}
			candidates = append(candidates, themeCandidate{author: ct.Author, slug: ct.Name})
		}
	}

	result.candidates = rankThemeCandidates(author, slug, candidates)
	return result
}

// gatherLocalThemeSuggestions looks for cached themes close to author/slug,
// using only the local cache - no hub calls. Used by cache-only commands
// (e.g. `stellar remove` reporting an identifier that isn't cached) where a
// hub lookup would be beside the point: the error already means "stellar
// has nothing cached for this", so the only honest source for "did you
// mean" here is what's actually on disk.
func gatherLocalThemeSuggestions(author, slug string) []string {
	var candidates []themeCandidate

	if cached, err := cache.ListCachedThemes(); err == nil {
		for _, id := range cached {
			ct, perr := theme.ParseIdentifier(id)
			if perr != nil {
				continue
			}
			candidates = append(candidates, themeCandidate{author: ct.Author, slug: ct.Name})
		}
	}

	return rankThemeCandidates(author, slug, candidates)
}

// damerauLevenshtein computes the optimal-string-alignment edit distance
// between a and b: insertions, deletions, substitutions, and transpositions
// of adjacent characters each cost 1. It operates on whatever runes are
// passed in (callers lowercase for case-insensitive matching), and is O(n*m)
// time and space - fine for the short theme-identifier-sized strings this
// package ever compares.
func damerauLevenshtein(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	la, lb := len(ra), len(rb)

	d := make([][]int, la+1)
	for i := range d {
		d[i] = make([]int, lb+1)
		d[i][0] = i
	}
	for j := 0; j <= lb; j++ {
		d[0][j] = j
	}

	for i := 1; i <= la; i++ {
		for j := 1; j <= lb; j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			del := d[i-1][j] + 1
			ins := d[i][j-1] + 1
			sub := d[i-1][j-1] + cost
			best := min(del, ins, sub)
			if i > 1 && j > 1 && ra[i-1] == rb[j-2] && ra[i-2] == rb[j-1] {
				best = min(best, d[i-2][j-2]+1)
			}
			d[i][j] = best
		}
	}

	return d[la][lb]
}

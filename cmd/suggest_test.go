package cmd

import "testing"

func TestDamerauLevenshtein(t *testing.T) {
	tests := []struct {
		name string
		a, b string
		want int
	}{
		{"identical strings", "ctp-blue", "ctp-blue", 0},
		{"empty vs non-empty", "", "abc", 3},
		{"single substitution", "ctp-blue", "ctp-blur", 1},
		{"single deletion", "ctp-blue", "ctp-blu", 1},
		{"single insertion", "ctp-blu", "ctp-blue", 1},
		{"adjacent transposition counts as one edit", "bleu", "blue", 1},
		{"completely different strings", "abc", "xyz", 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := damerauLevenshtein(tt.a, tt.b)
			if got != tt.want {
				t.Errorf("damerauLevenshtein(%q, %q) = %d, want %d", tt.a, tt.b, got, tt.want)
			}
			// Distance is symmetric.
			if rev := damerauLevenshtein(tt.b, tt.a); rev != got {
				t.Errorf("damerauLevenshtein(%q, %q) = %d, not symmetric with %d", tt.b, tt.a, rev, got)
			}
		})
	}
}

// Sibling themes share most of their name; a typo must suggest the one that
// was meant, not every theme with the same prefix.
func TestRankThemeCandidatesSkipsSiblings(t *testing.T) {
	candidates := []themeCandidate{
		{author: "a3chron", slug: "ctp-blue"},
		{author: "a3chron", slug: "ctp-red"},
		{author: "a3chron", slug: "ctp-green"},
	}
	got := rankThemeCandidates("a3chron", "ctp-bleu", candidates)
	if len(got) != 1 || got[0] != "a3chron/ctp-blue" {
		t.Errorf("rankThemeCandidates(ctp-bleu) = %v, want [a3chron/ctp-blue]", got)
	}
}

func TestSuggestionThreshold(t *testing.T) {
	tests := []struct {
		reference string
		want      int
	}{
		{"ab", 1},                     // len/3 == 0, floor is 1
		{"abc", 1},                    // len 3, 3/3 == 1
		{"ctp-bleu", 2},               // len 8, 8/3 == 2
		{"a-very-long-theme-name", 3}, // len 22, capped at 3
	}
	for _, tt := range tests {
		if got := suggestionThreshold(tt.reference); got != tt.want {
			t.Errorf("suggestionThreshold(%q) = %d, want %d", tt.reference, got, tt.want)
		}
	}
}

func TestCloseEnough(t *testing.T) {
	tests := []struct {
		name      string
		candidate string
		reference string
		wantOK    bool
	}{
		{"exact match", "ctp-blue", "ctp-blue", true},
		{"case-insensitive exact match", "CTP-Blue", "ctp-blue", true},
		{"one typo within threshold", "ctp-bleu", "ctp-blue", true},
		{"prefix with enough length", "ctp-blue-mocha", "ctp-blue", true},
		{"substring with enough length", "blue", "ctp-blue", true},
		{"too short to match on substring alone", "e", "ctp-blue", false},
		{"completely unrelated", "zzzzzzzzzz", "ctp-blue", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ok, _ := closeEnough(tt.candidate, tt.reference)
			if ok != tt.wantOK {
				t.Errorf("closeEnough(%q, %q) ok = %v, want %v", tt.candidate, tt.reference, ok, tt.wantOK)
			}
		})
	}
}

func TestClosestVersion(t *testing.T) {
	tests := []struct {
		name      string
		requested string
		available []string
		want      string
	}{
		{"nearest minor, same major", "1.2", []string{"1.0", "1.1"}, "1.1"},
		{"exact minor distance ties pick the lower diff", "1.0", []string{"1.5", "1.1"}, "1.1"},
		{"no shared major falls back to highest", "9.9", []string{"1.0", "1.1"}, "1.1"},
		{"unparsable requested version falls back to highest", "latest", []string{"2.0", "1.9"}, "2.0"},
		{"single available version", "3.0", []string{"1.0"}, "1.0"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := closestVersion(tt.requested, tt.available)
			if got != tt.want {
				t.Errorf("closestVersion(%q, %v) = %q, want %q", tt.requested, tt.available, got, tt.want)
			}
		})
	}
}

func TestSortVersionsAscending(t *testing.T) {
	got := sortVersionsAscending([]string{"1.2", "1.0", "1.1"})
	want := []string{"1.0", "1.1", "1.2"}
	if len(got) != len(want) {
		t.Fatalf("sortVersionsAscending length = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("sortVersionsAscending()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestRankThemeCandidates(t *testing.T) {
	t.Run("dedupes and excludes the identifier that was actually asked for", func(t *testing.T) {
		candidates := []themeCandidate{
			{author: "a3chron", slug: "ctp-blue"},
			{author: "a3chron", slug: "ctp-blue"}, // duplicate
			{author: "a3chron", slug: "ctp-bleu"}, // the exact (not-found) identifier itself
		}
		got := rankThemeCandidates("a3chron", "ctp-bleu", candidates)
		if len(got) != 1 || got[0] != "a3chron/ctp-blue" {
			t.Errorf("rankThemeCandidates() = %v, want [a3chron/ctp-blue]", got)
		}
	})

	t.Run("drops candidates that aren't close enough", func(t *testing.T) {
		candidates := []themeCandidate{
			{author: "someone", slug: "completely-unrelated"},
		}
		got := rankThemeCandidates("a3chron", "ctp-blue", candidates)
		if len(got) != 0 {
			t.Errorf("rankThemeCandidates() = %v, want none", got)
		}
	})

	t.Run("rejects a candidate too distant on both slug and full identifier", func(t *testing.T) {
		candidates := []themeCandidate{
			{author: "a3chron", slug: "ctp-bluee"}, // close: distance 1 from ctp-blue
			// Distant in a different author's namespace too, so the looser
			// full-identifier threshold (which scales with the longer
			// "author/slug" string) can't rescue it either.
			{author: "zzzzzzz", slug: "xxxxxxxx"},
		}
		got := rankThemeCandidates("a3chron", "ctp-blue", candidates)
		if len(got) != 1 || got[0] != "a3chron/ctp-bluee" {
			t.Errorf("rankThemeCandidates() = %v, want [a3chron/ctp-bluee]", got)
		}
	})

	t.Run("caps at maxSuggestions, best matches first", func(t *testing.T) {
		candidates := []themeCandidate{
			{author: "a3chron", slug: "ctp-bluee"},  // distance 1 from ctp-blue
			{author: "a3chron", slug: "ctp-blu"},    // distance 1 from ctp-blue
			{author: "a3chron", slug: "ctp-blueee"}, // distance 2 from ctp-blue
			{author: "a3chron", slug: "ctp-bluezz"}, // distance 2 from ctp-blue
		}
		got := rankThemeCandidates("a3chron", "ctp-blue", candidates)
		if len(got) != maxSuggestions {
			t.Fatalf("rankThemeCandidates() returned %d candidates, want %d: %v", len(got), maxSuggestions, got)
		}
		// The two distance-1 matches must be the top two, ahead of either
		// distance-2 candidate.
		if got[0] != "a3chron/ctp-bluee" && got[0] != "a3chron/ctp-blu" {
			t.Errorf("rankThemeCandidates()[0] = %v, want a closest (distance-1) match first", got[0])
		}
		if got[1] != "a3chron/ctp-bluee" && got[1] != "a3chron/ctp-blu" {
			t.Errorf("rankThemeCandidates()[1] = %v, want a closest (distance-1) match second", got[1])
		}
	})

	t.Run("a wrong-author match still ranks on slug closeness", func(t *testing.T) {
		candidates := []themeCandidate{
			{author: "someone-else", slug: "ctp-blue"},
		}
		got := rankThemeCandidates("wrongauthor", "ctp-blue", candidates)
		if len(got) != 1 || got[0] != "someone-else/ctp-blue" {
			t.Errorf("rankThemeCandidates() = %v, want [someone-else/ctp-blue]", got)
		}
	})
}

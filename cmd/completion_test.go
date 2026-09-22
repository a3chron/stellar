// Package cmd contains E2E tests for stellar CLI commands, including shell
// tab-completion (see internal/completion for the implementation these
// tests exercise end-to-end via the hidden cobra "__complete" command).
package cmd

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/a3chron/stellar/internal/completion"
	"github.com/a3chron/stellar/internal/testutil"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// enableOnlineCompletion opts the test in to always-on hub-backed completion
// for apply/preview/info - the default only reaches the hub when the local
// cache came up empty.
func enableOnlineCompletion(t *testing.T) {
	t.Helper()
	t.Setenv(completion.EnvOnline, "1")
}

// disableOnlineCompletion opts the test out of hub-backed completion
// entirely, including the default local-miss fallback.
func disableOnlineCompletion(t *testing.T, value string) {
	t.Helper()
	t.Setenv(completion.EnvOnline, value)
}

// runComplete invokes the hidden "__complete" command with args and returns
// the output split into lines: zero or more candidate lines, followed by a
// trailing ":<directive>" line (see cobra's completions.go - the directive
// integer is always the last line, following a single colon).
func runComplete(t *testing.T, args ...string) []string {
	t.Helper()
	lines, _ := runCompleteCapturingStderr(t, args...)
	return lines
}

// runCompleteCapturingStderr is runComplete plus whatever the command wrote to
// stderr, for the tests that assert a failed hub lookup stays completely
// silent (a shell shows completion stderr to the user, and stdout noise
// corrupts the candidate list outright).
func runCompleteCapturingStderr(t *testing.T, args ...string) ([]string, string) {
	t.Helper()

	cmd := NewRootCmd()
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetArgs(append([]string{cobra.ShellCompRequestCmd}, args...))

	err := cmd.Execute()
	require.NoError(t, err, "stderr: %s", errOut.String())

	trimmed := strings.TrimRight(out.String(), "\n")
	if trimmed == "" {
		return nil, errOut.String()
	}
	return strings.Split(trimmed, "\n"), errOut.String()
}

// assertQuietStderr checks that nothing but cobra's own trailing note
// ("Completion ended with directive: ...", which it writes on every
// __complete call) reached stderr. A failed hub lookup must never surface as
// an error in the user's shell.
func assertQuietStderr(t *testing.T, stderr string) {
	t.Helper()
	for _, line := range strings.Split(strings.TrimRight(stderr, "\n"), "\n") {
		if line == "" || strings.HasPrefix(line, "Completion ended with directive:") {
			continue
		}
		t.Errorf("unexpected completion stderr output: %q", line)
	}
}

// directiveLine returns the last line of a runComplete result (the
// ":<directive>" line).
func directiveLine(lines []string) string {
	if len(lines) == 0 {
		return ""
	}
	return lines[len(lines)-1]
}

// candidateLines returns every line except the trailing directive line.
func candidateLines(lines []string) []string {
	if len(lines) == 0 {
		return nil
	}
	return lines[:len(lines)-1]
}

func TestCompletion_EmptyInput_LocalAuthorsOnly(t *testing.T) {
	env := testutil.SetupTestEnv(t)
	resetFlags()

	mockAPI := testutil.CreateDefaultMockAPI()
	env.SetupMockAPI(mockAPI)

	env.CreateThemeFile("local", "mytheme", "1.0", testutil.SampleTOML())
	env.CreateThemeFile("testuser", "sample-theme", "1.2", testutil.SampleTOML())

	lines := runComplete(t, "apply", "")

	assert.Equal(t, ":6", directiveLine(lines))
	assert.Equal(t, []string{"local/\tlocal", "testuser/\tlocal"}, candidateLines(lines))
	assert.Equal(t, 0, mockAPI.TotalRequests())
}

func TestCompletion_LocalAuthorPrefix_SuppressesRemote(t *testing.T) {
	env := testutil.SetupTestEnv(t)
	resetFlags()

	mockAPI := testutil.CreateDefaultMockAPI()
	env.SetupMockAPI(mockAPI)

	env.CreateThemeFile("local", "mytheme", "1.0", testutil.SampleTOML())
	env.CreateThemeFile("testuser", "sample-theme", "1.2", testutil.SampleTOML())

	lines := runComplete(t, "apply", "lo")

	assert.Equal(t, []string{"local/\tlocal"}, candidateLines(lines))
	assert.Equal(t, 0, mockAPI.TotalRequests())
}

func TestCompletion_UnknownAuthorPrefix_FallsBackToHub(t *testing.T) {
	env := testutil.SetupTestEnv(t)
	resetFlags()
	enableOnlineCompletion(t)

	mockAPI := testutil.CreateDefaultMockAPI()
	env.SetupMockAPI(mockAPI)

	env.CreateThemeFile("local", "mytheme", "1.0", testutil.SampleTOML())

	lines := runComplete(t, "apply", "other")

	assert.Equal(t, []string{"otheruser/\thub"}, candidateLines(lines))
	assert.GreaterOrEqual(t, mockAPI.Requests("/api/themes"), 1)
}

func TestCompletion_AuthorSlash_LocalThenRemoteDeduped(t *testing.T) {
	env := testutil.SetupTestEnv(t)
	resetFlags()
	enableOnlineCompletion(t)

	mockAPI := testutil.CreateDefaultMockAPI()
	env.SetupMockAPI(mockAPI)

	// "sample-theme" exists both locally and on the hub (should be deduped,
	// kept as the local entry); "local-only" only exists locally;
	// "custom-theme" only exists on the hub.
	env.CreateThemeFile("testuser", "sample-theme", "1.2", testutil.SampleTOML())
	env.CreateThemeFile("testuser", "local-only", "1.0", testutil.SampleTOML())

	lines := runComplete(t, "apply", "testuser/")

	assert.Equal(t, ":36", directiveLine(lines))
	assert.Equal(t, []string{
		"testuser/local-only\tlocal",
		"testuser/sample-theme\tlocal",
		"testuser/custom-theme\thub",
	}, candidateLines(lines))
	assert.GreaterOrEqual(t, mockAPI.Requests("/api/themes"), 1)
}

func TestCompletion_RemoteOnlyAuthor_Slash(t *testing.T) {
	env := testutil.SetupTestEnv(t)
	resetFlags()
	enableOnlineCompletion(t)

	mockAPI := testutil.CreateDefaultMockAPI()
	env.SetupMockAPI(mockAPI)

	lines := runComplete(t, "apply", "otheruser/")

	assert.Equal(t, []string{"otheruser/ocean-theme\thub"}, candidateLines(lines))
}

func TestCompletion_VersionStage_LocalThenRemoteThenLatest(t *testing.T) {
	env := testutil.SetupTestEnv(t)
	resetFlags()
	enableOnlineCompletion(t)

	mockAPI := testutil.CreateDefaultMockAPI()
	env.SetupMockAPI(mockAPI)

	// Only 1.0 is cached locally; the hub (via CreateDefaultMockAPI) has
	// 1.2, 1.1 and 1.0 for testuser/sample-theme.
	env.CreateThemeFile("testuser", "sample-theme", "1.0", testutil.SampleTOML())

	lines := runComplete(t, "apply", "testuser/sample-theme@")

	assert.Equal(t, ":36", directiveLine(lines))
	assert.Equal(t, []string{
		"testuser/sample-theme@1.0\tlocal",
		"testuser/sample-theme@1.2\thub",
		"testuser/sample-theme@1.1\thub",
		"testuser/sample-theme@latest",
	}, candidateLines(lines))
}

func TestCompletion_Offline_DegradesToLocalOnly(t *testing.T) {
	testutil.SetupTestEnv(t)
	resetFlags()
	enableOnlineCompletion(t)

	// Nothing is listening on this port: connection should fail fast rather
	// than hang for the full 2s completion-client timeout.
	t.Setenv("STELLAR_API_URL", "http://127.0.0.1:1")

	lines := runComplete(t, "apply", "anything")

	assert.Empty(t, candidateLines(lines))
	assert.Equal(t, ":6", directiveLine(lines))
}

func TestCompletion_Remove_LocalOnly_NeverHitsAPI(t *testing.T) {
	env := testutil.SetupTestEnv(t)
	resetFlags()

	mockAPI := testutil.CreateDefaultMockAPI()
	env.SetupMockAPI(mockAPI)

	env.CreateThemeFile("testuser", "sample-theme", "1.2", testutil.SampleTOML())

	lines := runComplete(t, "remove", "testuser/")

	assert.Equal(t, []string{"testuser/sample-theme\tlocal"}, candidateLines(lines))
	assert.Equal(t, 0, mockAPI.TotalRequests())
}

func TestCompletion_Remove_ExcludesAlreadyTypedArgs(t *testing.T) {
	env := testutil.SetupTestEnv(t)
	resetFlags()

	env.CreateThemeFile("testuser", "sample-theme", "1.0", testutil.SampleTOML())
	env.CreateThemeFile("testuser", "other-theme", "1.0", testutil.SampleTOML())

	lines := runComplete(t, "remove", "testuser/sample-theme", "testuser/")

	assert.Equal(t, []string{"testuser/other-theme\tlocal"}, candidateLines(lines))
}

func TestCompletion_MissingStellarDir_NoErrorEmptyOutput(t *testing.T) {
	env := testutil.SetupTestEnv(t)
	resetFlags()

	require.NoError(t, os.RemoveAll(env.StellarDir))

	lines := runComplete(t, "apply", "")

	assert.Empty(t, candidateLines(lines))
}

func TestCompletion_BackupTheme_SkipsRemoteLookup(t *testing.T) {
	env := testutil.SetupTestEnv(t)
	resetFlags()
	enableOnlineCompletion(t)

	mockAPI := testutil.CreateDefaultMockAPI()
	env.SetupMockAPI(mockAPI)

	env.CreateThemeFile("someauthor", "backup", "1.0", testutil.SampleTOML())

	lines := runComplete(t, "apply", "someauthor/backup@")

	assert.Contains(t, candidateLines(lines), "someauthor/backup@1.0\tlocal")
	assert.Equal(t, 0, mockAPI.Requests("/api/someauthor/backup"))
}

func TestCompletion_EmptyAuthorSegment_NoCandidates(t *testing.T) {
	env := testutil.SetupTestEnv(t)
	resetFlags()
	enableOnlineCompletion(t)

	mockAPI := testutil.CreateDefaultMockAPI()
	env.SetupMockAPI(mockAPI)

	// "/x" would otherwise list author dirs as slugs of an empty author and
	// fire an unfiltered hub query - both must be suppressed.
	env.CreateThemeFile("xylo", "mytheme", "1.0", testutil.SampleTOML())

	lines := runComplete(t, "apply", "/x")

	assert.Empty(t, candidateLines(lines))
	assert.Equal(t, 0, mockAPI.TotalRequests())
}

func TestCompletion_EmptySlugSegment_NoCandidates(t *testing.T) {
	env := testutil.SetupTestEnv(t)
	resetFlags()
	enableOnlineCompletion(t)

	mockAPI := testutil.CreateDefaultMockAPI()
	env.SetupMockAPI(mockAPI)

	env.CreateThemeFile("alice", "rainbow", "1.0", testutil.SampleTOML())

	// "alice/@latest" would not parse, so "alice/@" must complete to nothing.
	lines := runComplete(t, "apply", "alice/@")

	assert.Empty(t, candidateLines(lines))
}

func TestCompletion_VersionStage_VPrefix(t *testing.T) {
	env := testutil.SetupTestEnv(t)
	resetFlags()
	enableOnlineCompletion(t)

	mockAPI := testutil.CreateDefaultMockAPI()
	env.SetupMockAPI(mockAPI)

	env.CreateThemeFile("testuser", "sample-theme", "1.0", testutil.SampleTOML())

	// The parser accepts "@v1.0", so "@v" should offer v-prefixed versions
	// (and never a nonsensical "vlatest").
	lines := runComplete(t, "apply", "testuser/sample-theme@v")

	assert.Equal(t, []string{
		"testuser/sample-theme@v1.0\tlocal",
		"testuser/sample-theme@v1.2\thub",
		"testuser/sample-theme@v1.1\thub",
	}, candidateLines(lines))
}

func TestCompletion_HubCanonicalAuthorCasing(t *testing.T) {
	env := testutil.SetupTestEnv(t)
	resetFlags()
	enableOnlineCompletion(t)

	mockAPI := testutil.CreateDefaultMockAPI()
	mockAPI.AddTheme(testutil.MockTheme{
		ID:     "cased-id",
		Author: "CasedUser",
		Slug:   "neon-theme",
		Name:   "Neon Theme",
		Versions: []testutil.MockVersion{
			{Version: "1.0", ConfigContent: testutil.SampleTOML(), CreatedAt: "2024-03-01T00:00:00Z"},
		},
	})
	env.SetupMockAPI(mockAPI)

	// The hub's /api/{author}/{slug} routes match author names exactly, and
	// every shell filters candidates against the typed word (bash's compgen
	// and zsh's compadd case-sensitively). So the hub's canonical casing
	// completes...
	lines := runComplete(t, "apply", "CasedUser/")
	assert.Equal(t, []string{"CasedUser/neon-theme\thub"}, candidateLines(lines))

	// ...while a differently-cased prefix suggests nothing, rather than
	// emitting a candidate that bash and zsh would silently discard.
	lines = runComplete(t, "apply", "caseduser/")
	assert.Empty(t, candidateLines(lines))
}

func TestCompletion_Default_UnknownAuthor_FallsBackToHub(t *testing.T) {
	env := testutil.SetupTestEnv(t)
	resetFlags()

	mockAPI := testutil.CreateDefaultMockAPI()
	env.SetupMockAPI(mockAPI)

	env.CreateThemeFile("local", "mytheme", "1.0", testutil.SampleTOML())

	// No STELLAR_COMPLETION_ONLINE set: "other" matches no cached author, so
	// the default local-then-hub mode falls back to the hub rather than
	// completing to nothing at all.
	lines := runComplete(t, "apply", "other")

	assert.Equal(t, []string{"otheruser/\thub"}, candidateLines(lines))
	assert.GreaterOrEqual(t, mockAPI.Requests("/api/themes"), 1)
}

func TestCompletion_Default_SlugStage_LocalMiss_FallsBackToHub(t *testing.T) {
	env := testutil.SetupTestEnv(t)
	resetFlags()

	mockAPI := testutil.CreateDefaultMockAPI()
	env.SetupMockAPI(mockAPI)

	// Cached: testuser/sample-theme only. The hub also has testuser/custom-theme
	// and otheruser/ocean-theme.
	env.CreateThemeFile("testuser", "sample-theme", "1.2", testutil.SampleTOML())

	// An author with nothing cached at all falls back...
	lines := runComplete(t, "apply", "otheruser/")
	assert.Equal(t, []string{"otheruser/ocean-theme\thub"}, candidateLines(lines))

	// ...and so does a cached author whose cached slugs don't match the typed
	// prefix: "isn't in local stuff" is judged per completion, not per author.
	lines = runComplete(t, "apply", "testuser/cus")
	assert.Equal(t, []string{"testuser/custom-theme\thub"}, candidateLines(lines))

	assert.GreaterOrEqual(t, mockAPI.Requests("/api/themes"), 2)
}

func TestCompletion_Default_VersionStage_UncachedTheme_FallsBackToHub(t *testing.T) {
	env := testutil.SetupTestEnv(t)
	resetFlags()

	mockAPI := testutil.CreateDefaultMockAPI()
	env.SetupMockAPI(mockAPI)

	// Nothing cached for testuser/sample-theme, so every version comes from
	// the hub. "latest" is still appended last, undecorated.
	lines := runComplete(t, "apply", "testuser/sample-theme@")

	assert.Equal(t, []string{
		"testuser/sample-theme@1.2\thub",
		"testuser/sample-theme@1.1\thub",
		"testuser/sample-theme@1.0\thub",
		"testuser/sample-theme@latest",
	}, candidateLines(lines))
	assert.GreaterOrEqual(t, mockAPI.Requests("/api/testuser/sample-theme"), 1)
}

func TestCompletion_Default_Offline_SilentlyDegradesToLocal(t *testing.T) {
	env := testutil.SetupTestEnv(t)
	resetFlags()

	// Nothing is listening on this port, so the default fallback lookup
	// fails. It must cost the user nothing: no error, no stray output, no
	// wait - just the local candidates (here, none).
	t.Setenv("STELLAR_API_URL", "http://127.0.0.1:1")
	env.CreateThemeFile("local", "mytheme", "1.0", testutil.SampleTOML())

	start := time.Now()
	lines, stderr := runCompleteCapturingStderr(t, "apply", "unknown-author")
	elapsed := time.Since(start)

	assert.Empty(t, candidateLines(lines))
	assert.Equal(t, ":6", directiveLine(lines))
	assertQuietStderr(t, stderr)
	assert.Less(t, elapsed, 5*time.Second, "a refused connection must fail fast")

	// The local cache still completes normally while offline.
	lines = runComplete(t, "apply", "loc")
	assert.Equal(t, []string{"local/\tlocal"}, candidateLines(lines))
}

func TestCompletion_Default_HangingHub_BoundedByFallbackTimeout(t *testing.T) {
	env := testutil.SetupTestEnv(t)
	resetFlags()

	// A hub that accepts the connection and then never answers is the case
	// the timeout exists for. The default fallback runs under a tighter
	// budget (800ms) than the opt-in path (2s) precisely because the user
	// never asked for it - so this must come back well under 2s.
	released := make(chan struct{})
	server := httptest.NewServer(
		http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
			<-released
		}),
	)
	// Cleanups run LIFO, so this releases the blocked handler *before*
	// server.Close() starts waiting for it.
	t.Cleanup(server.Close)
	t.Cleanup(func() { close(released) })
	t.Setenv("STELLAR_API_URL", server.URL)

	env.CreateThemeFile("local", "mytheme", "1.0", testutil.SampleTOML())

	start := time.Now()
	lines, stderr := runCompleteCapturingStderr(t, "apply", "unknown-author")
	elapsed := time.Since(start)

	assert.Less(t, elapsed, 1500*time.Millisecond,
		"the default fallback must be bounded by the sub-second budget, not the 2s opt-in one")
	// Guard against a vacuous pass: if the lookup never happened, this would
	// have returned instantly.
	assert.Greater(t, elapsed, 500*time.Millisecond, "the hub should actually have been queried")
	assert.Empty(t, candidateLines(lines))
	assertQuietStderr(t, stderr)
	assert.Equal(t, ":6", directiveLine(lines))
}

func TestCompletion_ExplicitOptOut_NeverQueriesHub(t *testing.T) {
	// Anything set but not recognised as "on" must mean local-only. A value
	// like "off" or "no" is someone asking for no network; falling through to
	// the default would do the exact opposite of what they asked, silently.
	for _, value := range []string{"0", "false", "off", "no", "False", "FALSE", " 0 "} {
		t.Run(value, func(t *testing.T) {
			env := testutil.SetupTestEnv(t)
			resetFlags()
			disableOnlineCompletion(t, value)

			mockAPI := testutil.CreateDefaultMockAPI()
			env.SetupMockAPI(mockAPI)

			// Every stage that would otherwise fall back must stay local.
			for _, arg := range []string{"other", "otheruser/", "testuser/sample-theme@"} {
				runComplete(t, "apply", arg)
			}

			assert.Equal(t, 0, mockAPI.TotalRequests())
		})
	}
}

func TestCompletion_Default_SlugStage_LocalOnlyNoNetwork(t *testing.T) {
	env := testutil.SetupTestEnv(t)
	resetFlags()

	mockAPI := testutil.CreateDefaultMockAPI()
	env.SetupMockAPI(mockAPI)

	env.CreateThemeFile("testuser", "sample-theme", "1.2", testutil.SampleTOML())

	// The hub also has testuser/custom-theme, but a local slug matched, so the
	// default mode spends no round trip. That's the deliberate tradeoff of
	// fallback-only: the fast path never pays, at the cost of not merging in
	// hub themes you don't have yet. STELLAR_COMPLETION_ONLINE=1 buys those
	// (see TestCompletion_AuthorSlash_LocalThenRemoteDeduped).
	lines := runComplete(t, "apply", "testuser/")

	assert.Equal(t, []string{"testuser/sample-theme\tlocal"}, candidateLines(lines))
	assert.Equal(t, 0, mockAPI.TotalRequests())
}

func TestCompletion_Default_VersionStage_LocalPlusLatestNoNetwork(t *testing.T) {
	env := testutil.SetupTestEnv(t)
	resetFlags()

	mockAPI := testutil.CreateDefaultMockAPI()
	env.SetupMockAPI(mockAPI)

	env.CreateThemeFile("testuser", "sample-theme", "1.0", testutil.SampleTOML())

	// One cached version is enough to keep the default mode offline, even
	// though the hub has 1.2 and 1.1 too.
	lines := runComplete(t, "apply", "testuser/sample-theme@")

	assert.Equal(t, []string{
		"testuser/sample-theme@1.0\tlocal",
		"testuser/sample-theme@latest",
	}, candidateLines(lines))
	assert.Equal(t, 0, mockAPI.TotalRequests())
}

func TestCompletion_HostileHubValues_Filtered(t *testing.T) {
	env := testutil.SetupTestEnv(t)
	resetFlags()
	enableOnlineCompletion(t)

	mockAPI := testutil.CreateDefaultMockAPI()
	// Author and slug outside the identifier character class (ANSI escape,
	// colon, space) must never reach the user's terminal as candidates.
	mockAPI.AddTheme(testutil.MockTheme{
		ID:     "evil-id",
		Author: "evil\x1b[31muser",
		Slug:   "bad:theme name",
		Name:   "Evil",
		Versions: []testutil.MockVersion{
			{Version: "1.0", ConfigContent: testutil.SampleTOML(), CreatedAt: "2024-03-01T00:00:00Z"},
		},
	})
	env.SetupMockAPI(mockAPI)

	lines := runComplete(t, "apply", "evil")

	assert.Empty(t, candidateLines(lines))
	// Guard against a vacuous pass: the hub must actually have been queried,
	// proving the empty result came from filtering, not from a dead network path.
	assert.GreaterOrEqual(t, mockAPI.Requests("/api/themes"), 1)
}

func TestCompletion_MalformedLocalCacheEntries_Filtered(t *testing.T) {
	env := testutil.SetupTestEnv(t)
	resetFlags()

	env.CreateThemeFile("good", "mytheme", "1.0", testutil.SampleTOML())
	// The cache is a plain directory a synced dotfiles checkout or an
	// extracted tarball can write to, so it gets the same treatment as an
	// untrusted hub response: names outside the identifier character class
	// must never be suggested - an ANSI escape especially, since candidates
	// are printed straight into the user's terminal.
	hostile := []string{".git", "my author"}
	if runtime.GOOS != "windows" {
		// Windows rejects control characters in filenames outright, so the
		// escape-sequence case can only be set up (and can only arise) on a
		// Unix filesystem.
		hostile = append(hostile, "evil\x1b[31muser")
	}
	for _, name := range hostile {
		require.NoError(t, os.MkdirAll(filepath.Join(env.StellarDir, name), 0o755))
	}

	lines := runComplete(t, "apply", "")

	assert.Equal(t, []string{"good/\tlocal"}, candidateLines(lines))
}

func TestCompletion_MalformedLocalSlugsAndVersions_Filtered(t *testing.T) {
	env := testutil.SetupTestEnv(t)
	resetFlags()

	env.CreateThemeFile("alice", "rainbow", "1.0", testutil.SampleTOML())
	require.NoError(t, os.MkdirAll(filepath.Join(env.StellarDir, "alice", "bad slug"), 0o755))
	// A theme directory can hold any *.toml name; only versions apply can
	// actually parse ("1.2", "latest") may be suggested.
	themeDir := filepath.Join(env.StellarDir, "alice", "rainbow")
	for _, name := range []string{"1.0.1.toml", "notes.toml", "latest.toml"} {
		require.NoError(t, os.WriteFile(filepath.Join(themeDir, name), []byte(testutil.SampleTOML()), 0o644))
	}

	lines := runComplete(t, "apply", "alice/")
	assert.Equal(t, []string{"alice/rainbow\tlocal"}, candidateLines(lines))

	lines = runComplete(t, "apply", "alice/rainbow@")
	assert.Equal(t, []string{"alice/rainbow@1.0\tlocal", "alice/rainbow@latest\tlocal"}, candidateLines(lines))
}

func TestCompletion_NoArgCommands_NoFileCompletion(t *testing.T) {
	env := testutil.SetupTestEnv(t)
	resetFlags()
	env.CreateThemeFile("alice", "rainbow", "1.0", testutil.SampleTOML())

	// Without a ValidArgsFunction these would return ShellCompDirectiveDefault
	// (":0") and the shell would offer the user's filenames instead.
	// ("version" is wired up the same way but registered on the package-level
	// rootCmd rather than in NewRootCmd, so it isn't reachable from here.)
	for _, name := range []string{"list", "clean", "current", "rollback", "update"} {
		lines := runComplete(t, name, "")
		assert.Equal(t, ":4", directiveLine(lines), "command %q should suppress file completion", name)
		assert.Empty(t, candidateLines(lines), "command %q should offer no candidates", name)
	}
}

func TestCompletion_HangingHub_BoundedByTimeout(t *testing.T) {
	env := testutil.SetupTestEnv(t)
	resetFlags()
	enableOnlineCompletion(t)

	// The 2s cap in api.NewCompletionClient is the property the whole feature
	// rests on: TAB must never hang on a slow hub. A refused connection (see
	// TestCompletion_Offline_DegradesToLocalOnly) fails instantly and so
	// doesn't exercise it - this server accepts and then never answers.
	released := make(chan struct{})
	server := httptest.NewServer(
		http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
			<-released
		}),
	)
	// Cleanups run LIFO, so this releases the blocked handler *before*
	// server.Close() starts waiting for it - registering them the other way
	// round deadlocks the test.
	t.Cleanup(server.Close)
	t.Cleanup(func() { close(released) })
	t.Setenv("STELLAR_API_URL", server.URL)

	env.CreateThemeFile("local", "mytheme", "1.0", testutil.SampleTOML())

	start := time.Now()
	lines := runComplete(t, "apply", "unknown-author")
	elapsed := time.Since(start)

	assert.Less(t, elapsed, 10*time.Second, "completion must not wait on a hanging hub")
	// Local candidates survive the failed lookup, and nothing leaks onto
	// stdout - stray output there corrupts what the shell parses.
	assert.Empty(t, candidateLines(lines))
	assert.Equal(t, ":6", directiveLine(lines))
}

func TestCompletion_AllIdentifierCommands_CompleteThemes(t *testing.T) {
	env := testutil.SetupTestEnv(t)
	resetFlags()

	env.CreateThemeFile("alice", "rainbow", "1.0", testutil.SampleTOML())

	// apply, preview and info all take one identifier; remove takes several.
	// Each needs its own ValidArgsFunction, and only apply/remove were
	// covered before.
	for _, name := range []string{"apply", "preview", "info", "remove"} {
		lines := runComplete(t, name, "ali")
		assert.Equal(t, []string{"alice/\tlocal"}, candidateLines(lines),
			"command %q should complete theme identifiers", name)
	}
}

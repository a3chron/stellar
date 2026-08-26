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
	"strings"
	"testing"
	"time"

	"github.com/a3chron/stellar/internal/completion"
	"github.com/a3chron/stellar/internal/testutil"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// enableOnlineCompletion opts the test in to hub-backed completion for
// apply/preview/info - the default is local-only for speed.
func enableOnlineCompletion(t *testing.T) {
	t.Helper()
	t.Setenv(completion.EnvOnline, "1")
}

// runComplete invokes the hidden "__complete" command with args and returns
// the output split into lines: zero or more candidate lines, followed by a
// trailing ":<directive>" line (see cobra's completions.go - the directive
// integer is always the last line, following a single colon).
func runComplete(t *testing.T, args ...string) []string {
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
		return nil
	}
	return strings.Split(trimmed, "\n")
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

func TestCompletion_Default_UnknownAuthor_LocalOnlyNoNetwork(t *testing.T) {
	env := testutil.SetupTestEnv(t)
	resetFlags()

	mockAPI := testutil.CreateDefaultMockAPI()
	env.SetupMockAPI(mockAPI)

	// Without STELLAR_COMPLETION_ONLINE, an unknown author prefix must NOT
	// fall back to the hub - completion stays instant and offline.
	lines := runComplete(t, "apply", "other")

	assert.Empty(t, candidateLines(lines))
	assert.Equal(t, 0, mockAPI.TotalRequests())
}

func TestCompletion_Default_SlugStage_LocalOnlyNoNetwork(t *testing.T) {
	env := testutil.SetupTestEnv(t)
	resetFlags()

	mockAPI := testutil.CreateDefaultMockAPI()
	env.SetupMockAPI(mockAPI)

	env.CreateThemeFile("testuser", "sample-theme", "1.2", testutil.SampleTOML())

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
	for _, name := range []string{".git", "my author", "evil\x1b[31muser"} {
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

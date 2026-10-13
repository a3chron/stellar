// E2E tests for `stellar auth` (+ login/status/logout), driven through
// NewRootCmd().Execute() against internal/testutil's mock API.
package cmd

import (
	"bytes"
	"io"
	"net"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/a3chron/stellar/internal/api"
	"github.com/a3chron/stellar/internal/auth"
	"github.com/a3chron/stellar/internal/paths"
	"github.com/a3chron/stellar/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// runStellar executes NewRootCmd with args, optionally replacing stdin, and
// returns everything written to stdout.
func runStellar(t *testing.T, in io.Reader, args ...string) (string, error) {
	t.Helper()
	var execErr error
	out := testutil.CaptureOutput(t, func() {
		cmd := NewRootCmd()
		cmd.SetArgs(args)
		cmd.SetOut(new(bytes.Buffer))
		if in != nil {
			cmd.SetIn(in)
		}
		execErr = cmd.Execute()
	})
	return out, execErr
}

// stubAuthSleep replaces authSleep with a no-op so auth e2e tests don't
// actually wait out the polling interval, restoring the original via
// t.Cleanup.
func stubAuthSleep(t *testing.T) {
	t.Helper()
	orig := authSleep
	authSleep = func(time.Duration) {}
	t.Cleanup(func() { authSleep = orig })
}

func TestE2E_AuthLogin(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		env := testutil.SetupTestEnv(t)
		resetFlags()
		stubAuthSleep(t)
		mockAPI := testutil.CreateDefaultMockAPI()
		env.SetupMockAPI(mockAPI)
		mockAPI.SetNextAuthApprove(2, "testuser", "Test User")

		out, err := runStellar(t, nil, "auth", "--no-browser")
		require.NoError(t, err)
		assert.Contains(t, out, "Logged in as")
		assert.Contains(t, out, "testuser")

		token, terr := auth.Token(env.MockServer.URL)
		require.NoError(t, terr)
		assert.NotEmpty(t, token)
	})

	t.Run("already logged in short-circuits without a new device flow", func(t *testing.T) {
		env := testutil.SetupTestEnv(t)
		resetFlags()
		stubAuthSleep(t)
		mockAPI := testutil.CreateDefaultMockAPI()
		env.SetupMockAPI(mockAPI)
		token := mockAPI.Token("testuser", "Test User")
		require.NoError(t, auth.Save(env.MockServer.URL, token))

		out, err := runStellar(t, nil, "auth", "--no-browser")
		require.NoError(t, err)
		assert.Contains(t, out, "Already logged in as")
		assert.Contains(t, out, "testuser")
	})

	t.Run("denied", func(t *testing.T) {
		env := testutil.SetupTestEnv(t)
		resetFlags()
		stubAuthSleep(t)
		mockAPI := testutil.CreateDefaultMockAPI()
		env.SetupMockAPI(mockAPI)
		mockAPI.SetNextAuthDeny(1)

		_, err := runStellar(t, nil, "auth", "--no-browser")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "denied")
	})

	t.Run("expired", func(t *testing.T) {
		env := testutil.SetupTestEnv(t)
		resetFlags()
		stubAuthSleep(t)
		mockAPI := testutil.CreateDefaultMockAPI()
		env.SetupMockAPI(mockAPI)
		mockAPI.SetNextAuthExpired()

		_, err := runStellar(t, nil, "auth", "--no-browser")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "expired")
	})

	t.Run("slow_down once still succeeds", func(t *testing.T) {
		env := testutil.SetupTestEnv(t)
		resetFlags()
		stubAuthSleep(t)
		mockAPI := testutil.CreateDefaultMockAPI()
		env.SetupMockAPI(mockAPI)
		mockAPI.SetNextAuthApprove(2, "testuser", "Test User")
		mockAPI.SetNextAuthSlowDownOnce()

		out, err := runStellar(t, nil, "auth", "--no-browser")
		require.NoError(t, err)
		assert.Contains(t, out, "Logged in as")
	})

	t.Run("credentials.json is 0600", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("unix permissions only")
		}
		env := testutil.SetupTestEnv(t)
		resetFlags()
		stubAuthSleep(t)
		mockAPI := testutil.CreateDefaultMockAPI()
		env.SetupMockAPI(mockAPI)
		mockAPI.SetNextAuthApprove(1, "testuser", "Test User")

		_, err := runStellar(t, nil, "auth", "--no-browser")
		require.NoError(t, err)

		info, serr := os.Stat(filepath.Join(env.StellarDir, "credentials.json"))
		require.NoError(t, serr)
		assert.Equal(t, os.FileMode(0600), info.Mode().Perm())
	})

	t.Run("STELLAR_TOKEN set warns that it takes precedence", func(t *testing.T) {
		env := testutil.SetupTestEnv(t)
		resetFlags()
		stubAuthSleep(t)
		mockAPI := testutil.CreateDefaultMockAPI()
		env.SetupMockAPI(mockAPI)
		mockAPI.SetNextAuthApprove(1, "testuser", "Test User")
		t.Setenv(paths.EnvToken, "stl_from_env")

		out, err := runStellar(t, nil, "auth", "--no-browser")
		require.NoError(t, err)
		assert.Contains(t, out, "STELLAR_TOKEN")
		assert.Contains(t, out, "takes precedence")
	})
}

func TestE2E_AuthStatus(t *testing.T) {
	t.Run("not logged in exits non-zero", func(t *testing.T) {
		env := testutil.SetupTestEnv(t)
		resetFlags()
		mockAPI := testutil.CreateDefaultMockAPI()
		env.SetupMockAPI(mockAPI)

		_, err := runStellar(t, nil, "auth", "status")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not logged in")
	})

	t.Run("logged in shows the username", func(t *testing.T) {
		env := testutil.SetupTestEnv(t)
		resetFlags()
		mockAPI := testutil.CreateDefaultMockAPI()
		env.SetupMockAPI(mockAPI)
		token := mockAPI.Token("testuser", "Test User")
		require.NoError(t, auth.Save(env.MockServer.URL, token))

		out, err := runStellar(t, nil, "auth", "status")
		require.NoError(t, err)
		assert.Contains(t, out, "testuser")
	})

	t.Run("revoked token reports not logged in", func(t *testing.T) {
		env := testutil.SetupTestEnv(t)
		resetFlags()
		mockAPI := testutil.CreateDefaultMockAPI()
		env.SetupMockAPI(mockAPI)
		require.NoError(t, auth.Save(env.MockServer.URL, "stl_not_a_real_token"))

		_, err := runStellar(t, nil, "auth", "status")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not logged in")
	})

	t.Run("STELLAR_TOKEN notes it's the source", func(t *testing.T) {
		env := testutil.SetupTestEnv(t)
		resetFlags()
		mockAPI := testutil.CreateDefaultMockAPI()
		env.SetupMockAPI(mockAPI)
		token := mockAPI.Token("testuser", "Test User")
		t.Setenv(paths.EnvToken, token)

		out, err := runStellar(t, nil, "auth", "status")
		require.NoError(t, err)
		assert.Contains(t, out, "STELLAR_TOKEN")
	})
}

func TestE2E_AuthLogout(t *testing.T) {
	env := testutil.SetupTestEnv(t)
	resetFlags()
	mockAPI := testutil.CreateDefaultMockAPI()
	env.SetupMockAPI(mockAPI)
	token := mockAPI.Token("testuser", "Test User")
	require.NoError(t, auth.Save(env.MockServer.URL, token))

	out, err := runStellar(t, nil, "auth", "logout")
	require.NoError(t, err)
	assert.Contains(t, out, "Logged out")

	remaining, terr := auth.Token(env.MockServer.URL)
	require.NoError(t, terr)
	assert.Empty(t, remaining)

	// The status command (and whoami) now see it as revoked too.
	_, err = runStellar(t, nil, "auth", "status")
	require.Error(t, err)
}

// TestE2E_AuthHubTooOld is the test Kurt's rollout-plan addendum asked for:
// against a hub deployment that predates CLI publishing (SetLegacyHub),
// every new-endpoint call must report a clear "doesn't support CLI
// publishing yet" instead of a misleading "Unauthorized"/"run stellar auth".
func TestE2E_AuthHubTooOld(t *testing.T) {
	t.Run("auth start", func(t *testing.T) {
		env := testutil.SetupTestEnv(t)
		resetFlags()
		stubAuthSleep(t)
		mockAPI := testutil.CreateDefaultMockAPI()
		env.SetupMockAPI(mockAPI)
		mockAPI.SetLegacyHub(true)

		out, err := runStellar(t, nil, "auth", "--no-browser")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "doesn't support CLI publishing yet")
		assert.NotContains(t, err.Error(), "Unauthorized")
		assert.NotContains(t, err.Error(), "run `stellar auth`")
		assert.NotContains(t, out, "Unauthorized")
	})

	t.Run("auth status with a stored token", func(t *testing.T) {
		env := testutil.SetupTestEnv(t)
		resetFlags()
		mockAPI := testutil.CreateDefaultMockAPI()
		env.SetupMockAPI(mockAPI)
		// Issue the token before flipping to legacy mode (the issuing route
		// itself doesn't exist on a legacy hub either, but the credential
		// could have been saved earlier against a hub that has since been
		// rolled back).
		token := mockAPI.Token("testuser", "Test User")
		require.NoError(t, auth.Save(env.MockServer.URL, token))
		mockAPI.SetLegacyHub(true)

		_, err := runStellar(t, nil, "auth", "status")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "doesn't support CLI publishing yet")
		assert.NotContains(t, err.Error(), "not logged in")
	})
}

// TestPollForToken_TransientOfflineDoesNotAbortLogin guards the fix for
// review finding A: a single transient transport failure (a WiFi blip) must
// not abort an otherwise-healthy login. authSleep is stubbed to start the
// real mock server (bound to the exact address the client is already
// talking to) right before the 3rd poll attempt - so the first two attempts
// genuinely fail at the transport level (connection refused, which
// api.wrapTransportError reports as api.ErrOffline), and the login must
// still succeed once the server comes up.
func TestPollForToken_TransientOfflineDoesNotAbortLogin(t *testing.T) {
	testutil.SetupTestEnv(t)
	resetFlags()

	mockAPI := testutil.CreateDefaultMockAPI()
	mockAPI.SetNextAuthApprove(1, "testuser", "Test User")

	// Start the server for real first, so StartDeviceAuth registers a
	// genuine device code the mock will recognize once it comes back - then
	// close it to free the port, simulating the connection dropping right
	// after start.
	server := httptest.NewServer(mockAPI)
	addr := server.Listener.Addr().String()
	require.NoError(t, os.Setenv(paths.EnvAPIURL, server.URL))
	t.Cleanup(func() { _ = os.Unsetenv(paths.EnvAPIURL) })

	start, serr := api.NewAuthedClient("").StartDeviceAuth()
	require.NoError(t, serr)
	server.Close()

	// From here, anything dialing addr gets a deterministic, instant
	// "connection refused" until the server is rebound to the same address
	// below.
	var revived *httptest.Server
	callCount := 0
	origSleep := authSleep
	authSleep = func(time.Duration) {
		callCount++
		if callCount == 3 {
			listener, lerr := net.Listen("tcp", addr)
			require.NoError(t, lerr)
			revived = httptest.NewUnstartedServer(mockAPI)
			revived.Listener = listener
			revived.Start()
		}
	}
	t.Cleanup(func() {
		authSleep = origSleep
		if revived != nil {
			revived.Close()
		}
	})

	token, _, username, perr := pollForToken(start)
	require.NoError(t, perr, "a transient transport failure must not abort the login")
	assert.NotEmpty(t, token)
	assert.Equal(t, "testuser", username)
	assert.GreaterOrEqual(t, callCount, 3, "must have retried through at least the two offline attempts")
}

// TestPollForToken_GivesUpAfterTooManyTransportFailures confirms a
// genuinely dead connection still fails, rather than spinning until the
// device code itself expires.
func TestPollForToken_GivesUpAfterTooManyTransportFailures(t *testing.T) {
	testutil.SetupTestEnv(t)
	resetFlags()

	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := l.Addr().String()
	require.NoError(t, l.Close())
	require.NoError(t, os.Setenv(paths.EnvAPIURL, "http://"+addr))
	t.Cleanup(func() { _ = os.Unsetenv(paths.EnvAPIURL) })

	origSleep := authSleep
	authSleep = func(time.Duration) {}
	t.Cleanup(func() { authSleep = origSleep })

	start := &api.StartAuthResponse{
		DeviceCode: "device-dead-test",
		UserCode:   "XXXX-DEAD",
		ExpiresIn:  600,
		Interval:   0,
	}

	_, _, _, perr := pollForToken(start)
	require.Error(t, perr)
	assert.Contains(t, perr.Error(), "offline")
}

func TestLoginLabel(t *testing.T) {
	assert.Equal(t, "a3chron", loginLabel("a3chron", "a3chron"))
	assert.Equal(t, "a3chron", loginLabel("A3chron", "a3chron"))
	assert.Equal(t, "a3chron", loginLabel("", "a3chron"))
	assert.Equal(t, "Kurt (@a3chron)", loginLabel("Kurt", "a3chron"))
	assert.Equal(t, "Kurt (@a3chron)", loginLabel("Ku\x1brt", "a3chron"))
}

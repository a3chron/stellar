// cmd/auth.go implements `stellar auth` (and its login/status/logout
// subcommands): the browser device-login flow described in the plan's "Auth"
// section - start a request, show the user a code and a link, open their
// browser, and poll until they approve (or deny/expire) it.
package cmd

import (
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/a3chron/stellar/internal/api"
	"github.com/a3chron/stellar/internal/auth"
	"github.com/a3chron/stellar/internal/config"
	"github.com/a3chron/stellar/internal/paths"
	"github.com/a3chron/stellar/internal/term"
	"github.com/fatih/color"
	"github.com/spf13/cobra"
)

var authNoBrowser bool

// authSleep is the sleep used between polls of POST /api/cli/auth/token - a
// package var purely so tests can stub it out (and assert on how many times
// it's called, and with what duration) instead of a real E2E test taking as
// long as the real polling interval.
var authSleep = time.Sleep

// authBaseURL returns the base URL auth should key its stored credential
// against - the same one api.NewClient()/NewAuthedClient() talk to.
func authBaseURL() string {
	return paths.APIURL(api.BaseURL)
}

// whoamiWithToken is a small wrapper so every call site shares the exact
// same "build an authed client, call whoami" sequence.
func whoamiWithToken(token string) (*api.WhoamiResponse, error) {
	return api.NewAuthedClient(token).Whoami()
}

// displayName prefers the human name, falling back to the handle - and
// strips control characters either way, since both values come straight
// from the hub.
func displayName(who *api.WhoamiResponse) string {
	if who.Name != "" {
		return term.SanitizeForTerminal(who.Name)
	}
	return term.SanitizeForTerminal(who.Username)
}

// cacheUsername best-effort saves username into config.json so `stellar
// new`/`stellar fork` can pick a default author without a whoami round trip
// on every run (see config.Config.Username). Failing to save is never fatal
// here - it only degrades a future command's default, it can't lose data.
func cacheUsername(username string) {
	cfg, err := config.Load()
	if err != nil {
		cfg = &config.Config{}
	}
	if cfg.Username == username {
		return
	}
	cfg.Username = username
	if err := cfg.Save(); err != nil {
		color.Yellow("Warning: couldn't cache your username: %v", err)
	}
}

var authCmd = &cobra.Command{
	Use:   "auth",
	Short: "Log in to stellar-hub",
	Long: `Log in to stellar-hub for stellar publish.

Opens a browser to approve this device, then polls stellar-hub until you do.
The credential is saved to credentials.json in your stellar home directory.`,
	Example: `  stellar auth
  stellar auth --no-browser`,
	Args: argsWithUsage(cobra.NoArgs),
	RunE: runAuthLogin,
}

var authLoginCmd = &cobra.Command{
	Use:     "login",
	Short:   "Log in to stellar-hub (same as `stellar auth`)",
	Args:    argsWithUsage(cobra.NoArgs),
	Example: `  stellar auth login`,
	RunE:    runAuthLogin,
}

var authStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show who stellar is logged in as",
	Args:  argsWithUsage(cobra.NoArgs),
	RunE: func(cmd *cobra.Command, args []string) error {
		baseURL := authBaseURL()
		token, err := auth.Token(baseURL)
		if err != nil {
			return err
		}
		if token == "" {
			return notLoggedInError()
		}

		who, err := whoamiWithToken(token)
		if err != nil {
			return wrapHubAPIError(err)
		}
		cacheUsername(who.Username)

		color.Green("Logged in as %s (@%s)", displayName(who), term.SanitizeForTerminal(who.Username))
		if os.Getenv(paths.EnvToken) != "" {
			color.HiBlack("(using STELLAR_TOKEN, not the stored credential)")
		}
		return nil
	},
}

var authLogoutCmd = &cobra.Command{
	Use:   "logout",
	Short: "Revoke the stored stellar-hub login",
	Args:  argsWithUsage(cobra.NoArgs),
	RunE: func(cmd *cobra.Command, args []string) error {
		baseURL := authBaseURL()
		token, err := auth.Token(baseURL)
		if err != nil {
			return err
		}

		if token != "" {
			// Best-effort: a 401 already counts as success per the contract
			// (api.Client.RevokeToken returns nil for it), and any other
			// failure (offline, an older hub) still shouldn't stop the local
			// credential from being forgotten - there would be nothing left
			// to retry revoking anyway once it's deleted below.
			if rerr := api.NewAuthedClient(token).RevokeToken(); rerr != nil {
				color.Yellow("Warning: couldn't revoke the token on stellar-hub: %v", wrapHubAPIError(rerr))
			}
		}

		if err := auth.Delete(baseURL); err != nil {
			return err
		}

		color.Green("Logged out")
		return nil
	},
}

// runAuthLogin is shared by `stellar auth` and `stellar auth login`.
func runAuthLogin(cmd *cobra.Command, args []string) error {
	baseURL := authBaseURL()

	if os.Getenv(paths.EnvToken) != "" {
		color.Yellow("STELLAR_TOKEN is set and takes precedence over this login - unset it to use the new one.")
	}

	if token, err := auth.Token(baseURL); err == nil && token != "" {
		if who, werr := whoamiWithToken(token); werr == nil {
			cacheUsername(who.Username)
			color.Green("Already logged in as %s (@%s)", displayName(who), term.SanitizeForTerminal(who.Username))
			return nil
		} else if !errors.Is(werr, api.ErrUnauthorized) {
			// Anything other than "this token is no good" (offline, an older
			// hub, ...) means a fresh login attempt would fail the exact same
			// way - report it now instead of pretending nothing is stored.
			return wrapHubAPIError(werr)
		}
		// ErrUnauthorized: the stored token was revoked or is otherwise bad -
		// fall through and get a new one.
	}

	client := api.NewAuthedClient("")
	start, err := client.StartDeviceAuth()
	if err != nil {
		return wrapHubAPIError(err)
	}

	fmt.Printf("Your code: %s\n", color.New(color.Bold).Sprint(start.UserCode))
	link := start.VerificationURLComplete
	if link == "" {
		link = start.VerificationURL
	}
	fmt.Printf("Approve this device at: %s%s\n", term.Hyperlink(link, link), color.HiBlackString(term.ClickHint()))

	if !authNoBrowser {
		openBrowser(link)
	}

	color.Cyan("Waiting for you to approve in the browser...")

	token, name, username, err := pollForToken(start)
	if err != nil {
		return err
	}

	if err := auth.Save(baseURL, token); err != nil {
		return fmt.Errorf("logged in, but failed to save the credential: %w", err)
	}
	cacheUsername(username)

	display := name
	if display == "" {
		display = username
	}
	color.Green("✓ Logged in as %s (@%s)", term.SanitizeForTerminal(display), term.SanitizeForTerminal(username))
	return nil
}

// pollForToken polls POST /api/cli/auth/token (via authSleep between
// attempts) until it gets a token, the user denies/the code expires, or the
// hub reports a failure that isn't one of the documented "keep waiting"
// codes. There is deliberately no "press Enter to check now" path: a stdin
// goroutine reading for that would race with (and could steal input from)
// the very next prompt `stellar publish` shows when it logs in inline.
// maxConsecutiveAuthTransportFailures bounds how many consecutive transport
// failures (DNS hiccup, a dropped WiFi connection, ...) pollForToken rides
// out before giving up - a single blip must not abort an otherwise-healthy
// login, but a genuinely dead connection still has to fail eventually
// rather than spin silently until the code itself expires.
const maxConsecutiveAuthTransportFailures = 5

func pollForToken(start *api.StartAuthResponse) (token, name, username string, err error) {
	client := api.NewAuthedClient("")
	interval := time.Duration(start.Interval) * time.Second
	if interval <= 0 {
		interval = 3 * time.Second
	}
	deadline := time.Now().Add(time.Duration(start.ExpiresIn) * time.Second)

	consecutiveTransportFailures := 0

	for {
		if !time.Now().Before(deadline) {
			return "", "", "", fmt.Errorf("login code expired - run `stellar auth` again")
		}

		authSleep(interval)

		resp, perr := client.PollDeviceToken(start.DeviceCode)
		if perr == nil {
			return resp.Token, resp.User.Name, resp.User.Username, nil
		}

		// A transient transport failure (offline for a moment, a dropped
		// connection) is not the same as the hub actively telling us
		// something - keep polling through the deadline rather than aborting
		// the whole login on one blip, but don't spin forever on a
		// genuinely dead connection either.
		if errors.Is(perr, api.ErrOffline) {
			consecutiveTransportFailures++
			if consecutiveTransportFailures >= maxConsecutiveAuthTransportFailures {
				return "", "", "", fmt.Errorf("can't reach stellar-hub (are you offline?): %w", perr)
			}
			continue
		}
		consecutiveTransportFailures = 0

		var apiErr *api.APIError
		if errors.As(perr, &apiErr) {
			switch apiErr.Code {
			case "authorization_pending":
				continue
			case "slow_down":
				interval += 2 * time.Second
				continue
			case "access_denied":
				return "", "", "", fmt.Errorf("login was denied")
			case "expired_token":
				return "", "", "", fmt.Errorf("login code expired - run `stellar auth` again")
			case "invalid_grant":
				return "", "", "", fmt.Errorf("invalid login code - run `stellar auth` again")
			default:
				return "", "", "", wrapHubAPIError(perr)
			}
		}
		return "", "", "", wrapHubAPIError(perr)
	}
}

func init() {
	authCmd.Flags().BoolVar(&authNoBrowser, "no-browser", false, "Don't try to open a browser automatically")
	authLoginCmd.Flags().BoolVar(&authNoBrowser, "no-browser", false, "Don't try to open a browser automatically")

	authCmd.AddCommand(authLoginCmd)
	authCmd.AddCommand(authStatusCmd)
	authCmd.AddCommand(authLogoutCmd)

	authCmd.ValidArgsFunction = cobra.NoFileCompletions
	authLoginCmd.ValidArgsFunction = cobra.NoFileCompletions
	authStatusCmd.ValidArgsFunction = cobra.NoFileCompletions
	authLogoutCmd.ValidArgsFunction = cobra.NoFileCompletions
}

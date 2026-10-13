// cmd/auth.go implements `stellar auth` (and its login/status/logout
// subcommands): the browser device-login flow described in the plan's "Auth"
// section - start a request, show the user a code and a link, open their
// browser, and poll until they approve (or deny/expire) it.
package cmd

import (
	"errors"
	"fmt"
	"os"
	"strings"
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

// loginLabel is how the logged-in account is shown: "Kurt (@a3chron)", or
// just the handle when the display name is empty or the same as it (a GitHub
// login usually is). Control characters are stripped from both, since they
// come straight from the hub.
func loginLabel(name, username string) string {
	username = term.SanitizeForTerminal(username)
	name = term.SanitizeForTerminal(name)
	if name == "" || strings.EqualFold(name, username) {
		return username
	}
	return fmt.Sprintf("%s (@%s)", name, username)
}

// cacheUsername best-effort saves username into config.json so `stellar
// new`/`stellar fork` can pick a default author without a whoami round trip
// on every run (see config.Config.Username). Failing to save is never fatal
// here - it only degrades a future command's default, it can't lose data.
func cacheUsername(username string) {
	cfg, err := config.Load()
	if err != nil {
		// Don't save a fresh empty config over one we merely failed to read -
		// that would silently wipe currentTheme/authoredThemes/downloaded.
		// Caching the username is purely a convenience, so just warn.
		color.Yellow("Warning: couldn't cache your username: %v", err)
		return
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
	Long: `Log in to stellar-hub so you can publish themes from the terminal.

Opens a browser to approve this device, then polls stellar-hub until you do.
The credential is saved to credentials.json in your stellar home directory.`,
	Example: `  stellar auth
  stellar auth --no-browser`,
	Args: argsWithUsage(cobra.NoArgs),
	RunE: runAuthLogin,
}

var authLoginCmd = &cobra.Command{
	Use:     "login",
	Short:   "Log in to stellar-hub (same as \"stellar auth\")",
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

		color.Green("✓ Logged in as %s", loginLabel(who.Name, who.Username))
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
		// StoredToken, not Token: logout must act on the credential actually
		// saved to disk, never on a STELLAR_TOKEN override - revoking and
		// deleting "the token" when STELLAR_TOKEN is set would otherwise
		// either do nothing (STELLAR_TOKEN isn't stored, so there'd be
		// nothing found to delete) or, worse, be mistaken for the stored
		// entry and wrongly reported as logged out.
		token, err := auth.StoredToken(baseURL)
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
			if err := auth.Delete(baseURL); err != nil {
				return err
			}
			color.Green("✓ Logged out")
		} else {
			color.Yellow("Not logged in to %s - nothing to log out of.", baseURL)
		}

		if os.Getenv(paths.EnvToken) != "" {
			color.HiBlack("STELLAR_TOKEN is still set - it was not revoked or changed; unset it yourself if you're done with it.")
		}
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
			color.Green("✓ Already logged in as %s", loginLabel(who.Name, who.Username))
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

	userCode := term.SanitizeForTerminal(start.UserCode)
	fmt.Printf("Your code: %s\n", color.New(color.Bold).Sprint(userCode))
	link := start.VerificationURLComplete
	if link == "" {
		link = start.VerificationURL
	}
	fmt.Printf("Approve this device at: %s%s\n", term.Hyperlink(link, link), color.HiBlackString(term.ClickHint()))

	if !authNoBrowser {
		openBrowser(link)
	}

	stop := startSpinner(color.CyanString("Waiting for you to approve in the browser..."))
	token, name, username, err := pollForToken(start)
	stop()
	if err != nil {
		return err
	}

	if err := auth.Save(baseURL, token); err != nil {
		return fmt.Errorf("logged in, but failed to save the credential: %w", err)
	}
	cacheUsername(username)

	color.Green("✓ Logged in as %s", loginLabel(name, username))
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

		// A transient failure - offline for a moment, a dropped connection,
		// or the hub itself answering with a 5xx (a deploy in progress, a
		// gateway timeout) - is not the hub actively telling us something
		// about the login; keep polling through the deadline rather than
		// aborting on one blip, but don't spin forever on a genuinely dead
		// connection or a hub that's down for good either.
		var apiErr *api.APIError
		isServerError := errors.As(perr, &apiErr) && apiErr.Status >= 500
		if errors.Is(perr, api.ErrOffline) || isServerError {
			consecutiveTransportFailures++
			if consecutiveTransportFailures >= maxConsecutiveAuthTransportFailures {
				return "", "", "", wrapHubAPIError(perr)
			}
			continue
		}
		consecutiveTransportFailures = 0

		if apiErr != nil {
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

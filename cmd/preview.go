// cmd/preview.go
package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/a3chron/stellar/internal/api"
	"github.com/a3chron/stellar/internal/cache"
	"github.com/a3chron/stellar/internal/theme"
	"github.com/fatih/color"
	"github.com/spf13/cobra"
)

// startTerminal is the single seam through which preview ever actually
// launches a terminal-emulator subprocess (Linux) or hands a script to
// osascript (macOS/spawnMacTerminal). Both spawn paths call this instead of
// cmd.Start() directly.
//
// This exists so `go test` can never pop open a real terminal window on
// whatever machine runs the suite. It used to be a real risk: preview honors
// $TERMINAL/--terminal and matches terminal binaries actually present on
// $PATH, so on a real desktop session (a maintainer's machine with
// $DISPLAY/$WAYLAND_DISPLAY set and e.g. kitty/xterm installed - not a
// headless CI box) an ordinary `stellar preview ...` E2E test could
// successfully exec a real terminal emulator and leave a window open.
//
// The default implementation below refuses to spawn anything whenever
// testing.Testing() reports we're running inside a test binary - this is a
// property of the process, not of $PATH/$TERMINAL/$DISPLAY, so it holds
// regardless of the host environment and regardless of whether any given
// test remembers to guard against it itself. A test that specifically wants
// to exercise "a terminal was actually started" can still override this var
// with its own recorder and restore it afterward; nothing else needs to.
var startTerminal = func(cmd *exec.Cmd) error {
	if testing.Testing() {
		return fmt.Errorf("stellar test guard: refusing to spawn a real terminal process (%s) under go test", cmd.Path)
	}
	return cmd.Start()
}

var (
	previewForce    bool
	previewShell    string
	previewTerminal string
)

var previewCmd = &cobra.Command{
	Use:   "preview [author/theme[@version]]",
	Short: "Preview a theme in a new terminal window",
	Example: `  stellar preview a3chron/ctp-red
  stellar preview a3chron/ctp-red --shell fish
  stellar preview a3chron/ctp-red --terminal kitty`,
	Args: argsWithUsage(cobra.ExactArgs(1)),
	RunE: func(cmd *cobra.Command, args []string) error {
		identifier := args[0]

		// Parse and download if needed
		t, err := theme.ParseIdentifier(identifier)
		if err != nil {
			return err
		}

		client := api.NewClient()

		// Resolve version if not explicitly specified
		if t.NeedsVersionResolution() {
			themeDir, _ := t.CacheDir()
			localVer, localErr := theme.FindLatestLocalVersion(themeDir)
			hasLocalCache := localErr == nil

			// A literal "latest.toml" from an older build is not a concrete
			// answer; fall through rather than preview a stale alias.
			if hasLocalCache && localVer != theme.LatestVersion {
				t.Version = localVer
			} else {
				// Check /tmp cache before hitting the API
				tmpVer, tmpErr := theme.FindLatestLocalVersion(cache.TmpCacheDir(t))
				if tmpErr == nil {
					t.Version = tmpVer
				} else {
					// No local cache - check online for latest version
					info, infoErr := client.GetThemeInfo(t.Author, t.Name)
					if infoErr == nil && len(info.Versions) > 0 {
						t.Version = info.Versions[0].Version
					} else {
						return hubUnreachableError(t, infoErr)
					}
				}
			}
		}

		// Determine theme path: main cache → tmp cache → download to tmp,
		// validating whatever we end up with before it's ever handed to a
		// shell - a preview runs starship exactly like a real prompt, so
		// invalid TOML or [custom] commands are just as much a concern here
		// as they are for apply/rollback.
		var themePath string
		var validationResult theme.ValidationResult

		switch {
		case cache.ThemeExists(t):
			themePath, err = t.CachePath()
			if err != nil {
				return err
			}
			validationResult, err = validateOnDiskTheme(t, themePath, previewForce)
		case cache.TmpThemeExists(t):
			themePath = cache.TmpCachePath(t)
			validationResult, err = validateOnDiskTheme(t, themePath, previewForce)
		default:
			color.Yellow("Downloading %s...", t)
			var content string
			content, err = client.FetchThemeConfig(t.Author, t.Name, t.Version)
			if err != nil {
				return downloadError("preview", client, t, err)
			}
			validationResult, err = theme.ValidateConfigContent(content)
			if err != nil {
				return err
			}
			if !validationResult.Valid {
				return validationResult.Error
			}
			if err := cache.SaveThemeToTmp(t, content); err != nil {
				return err
			}
			themePath = cache.TmpCachePath(t)
		}
		if err != nil {
			return err
		}

		if err := confirmCustomCommands(t, validationResult, previewForce, themePath, "preview", "previewed"); err != nil {
			return err
		}

		// Spawn terminal
		if err := spawnTerminalWithEnv(themePath, t.String(), previewShell); err != nil {
			return err
		}

		color.Cyan("Theme: %s", t)

		return nil
	},
}

func init() {
	previewCmd.Flags().BoolVarP(&previewForce, "force", "f", false,
		"Skip the custom-command confirmation, and skip TOML validation (with a warning) for a theme already on disk - freshly downloaded content is still always validated")
	previewCmd.Flags().StringVar(&previewShell, "shell", "", "Shell to run in the preview (defaults to $SHELL, falling back to fish/zsh/bash)")
	previewCmd.Flags().StringVar(&previewTerminal, "terminal", "",
		"Terminal emulator to use for the preview (defaults to $TERMINAL, falling back to common terminals). Ignored on macOS, which always uses Terminal.app/iTerm. If not found on PATH, a warning is printed and stellar falls back to $TERMINAL/known terminals")
}

// selectPreviewShell picks which shell to run inside the preview terminal.
// explicit (the --shell flag) wins outright. Otherwise the user's own
// $SHELL is preferred - it's their actual login shell, not necessarily fish
// or zsh - falling back to fish, then zsh, then bash, then a bare "sh" if
// none of those are on PATH either. lookPath is injected so tests can fake
// which shells "exist" without touching the real PATH.
func selectPreviewShell(explicit, envShell string, lookPath func(string) (string, error)) string {
	if explicit != "" {
		return explicit
	}
	if envShell != "" {
		if _, err := lookPath(envShell); err == nil {
			return envShell
		}
	}
	for _, candidate := range []string{"fish", "zsh", "bash"} {
		if _, err := lookPath(candidate); err == nil {
			return candidate
		}
	}
	return "sh"
}

// shellArgsFor returns the argv used to start shell as a login shell inside
// the preview terminal, recognizing fish/zsh/bash (by base name, so a full
// path like "/usr/bin/fish" still gets "-l") and otherwise just running the
// given command as-is.
func shellArgsFor(shell string) []string {
	switch filepath.Base(shell) {
	case "fish", "zsh", "bash":
		return []string{shell, "-l"}
	default:
		return []string{shell}
	}
}

// knownTerminal is a terminal emulator preview knows the invocation
// convention for.
type knownTerminal struct {
	name string
	args func(shellArgs []string) []string
}

// knownTerminals is tried in order after any explicit/$TERMINAL choice.
var knownTerminals = []knownTerminal{
	{
		name: "wezterm",
		args: func(shellArgs []string) []string {
			return append([]string{"start", "--"}, shellArgs...)
		},
	},
	{
		name: "alacritty",
		args: func(shellArgs []string) []string {
			return append([]string{"-e"}, shellArgs...)
		},
	},
	{
		name: "ghostty",
		args: func(shellArgs []string) []string {
			return append([]string{"-e"}, shellArgs...)
		},
	},
	{
		name: "kitty",
		args: func(shellArgs []string) []string {
			return append([]string{"-e"}, shellArgs...)
		},
	},
	{
		name: "foot",
		args: func(shellArgs []string) []string {
			return shellArgs
		},
	},
	{
		name: "kgx",
		args: func(shellArgs []string) []string {
			return shellArgs
		},
	},
	{
		name: "gnome-terminal",
		args: func(shellArgs []string) []string {
			return append([]string{"--"}, shellArgs...)
		},
	},
	{
		name: "tilix",
		args: func(shellArgs []string) []string {
			return append([]string{"-e"}, shellArgs...)
		},
	},
	{
		name: "konsole",
		args: func(shellArgs []string) []string {
			return append([]string{"-e"}, shellArgs...)
		},
	},
	{
		name: "xfce4-terminal",
		args: func(shellArgs []string) []string {
			return append([]string{"-e"}, shellArgs...)
		},
	},
	{
		name: "xterm",
		args: func(shellArgs []string) []string {
			return append([]string{"-e"}, shellArgs...)
		},
	},
}

// previewTerminalCandidates orders the terminal names worth trying: an
// explicit --terminal flag first, then $TERMINAL, then the known list -
// deduplicated so a user's $TERMINAL that's already in the known list isn't
// tried twice.
func previewTerminalCandidates(explicit, envTerminal string) []string {
	seen := make(map[string]bool, len(knownTerminals)+2)
	var out []string
	add := func(name string) {
		if name == "" || seen[name] {
			return
		}
		seen[name] = true
		out = append(out, name)
	}
	add(explicit)
	add(envTerminal)
	for _, kt := range knownTerminals {
		add(kt.name)
	}
	return out
}

// argsForTerminal returns the argv (excluding the terminal's own binary name)
// used to make terminal run shellArgs. Known terminals use their documented
// invocation; an unrecognized one (from --terminal or $TERMINAL) defaults to
// "-e", the convention the large majority of terminal emulators support for
// "run this command and exit".
//
// Matching is done on filepath.Base(name): --terminal and $TERMINAL are
// often a full path (e.g. TERMINAL=/usr/bin/wezterm), and comparing the raw
// value against the known list's short names ("wezterm") would otherwise
// always miss, silently falling back to the generic "-e" convention even for
// a terminal stellar knows a better invocation for.
func argsForTerminal(name string, shellArgs []string) []string {
	base := filepath.Base(name)
	for _, kt := range knownTerminals {
		if kt.name == base {
			return kt.args(shellArgs)
		}
	}
	return append([]string{"-e"}, shellArgs...)
}

func spawnTerminalWithEnv(starshipConfig, themeName, shellFlag string) error {
	shell := selectPreviewShell(shellFlag, os.Getenv("SHELL"), exec.LookPath)

	switch runtime.GOOS {
	case "darwin": // macOS
		return spawnMacTerminal(starshipConfig, shell)
	case "linux":
		return spawnLinuxTerminal(starshipConfig, themeName, shell)
	default:
		// Includes windows: spawning a Windows Terminal/PowerShell window
		// isn't implemented yet (see README TODOs), so hand the user the
		// exact command to preview manually instead of just failing.
		return manualPreviewFallback(starshipConfig, shell)
	}
}

func spawnMacTerminal(starshipConfig, shell string) error {
	termProgram := os.Getenv("TERM_PROGRAM")

	var script string
	name := "Terminal"
	if termProgram == "iTerm.app" {
		name = "iTerm"
		script = fmt.Sprintf(`
			tell application "iTerm"
				create window with default profile
				tell current session of current window
					write text "export STARSHIP_CONFIG='%s' && exec %s -l"
				end tell
			end tell
		`, starshipConfig, shell)
	} else {
		script = fmt.Sprintf(`
			tell application "Terminal"
				do script "export STARSHIP_CONFIG='%s' && exec %s -l"
				activate
			end tell
		`, starshipConfig, shell)
	}

	cmd := exec.Command("osascript", "-e", script)
	if err := startTerminal(cmd); err != nil {
		return manualPreviewFallback(starshipConfig, shell)
	}
	color.White("Opening preview in %s...", name)
	return nil
}

func spawnLinuxTerminal(starshipConfig, themeName, shell string) error {
	shellArgs := shellArgsFor(shell)

	env := os.Environ()
	env = append(env,
		fmt.Sprintf("STARSHIP_CONFIG=%s", starshipConfig),
		"STARSHIP_LOG=error",
		"STARSHIP_CACHE=/tmp/starship-preview",
	)

	// An explicit --terminal that isn't actually on PATH is worth calling out
	// on its own: silently falling through to $TERMINAL/the known list (the
	// generic loop below already does that) could otherwise look like
	// stellar just ignored the flag.
	if previewTerminal != "" {
		if _, err := exec.LookPath(previewTerminal); err != nil {
			color.Yellow("--terminal %s not found on PATH, falling back to $TERMINAL/known terminals.", previewTerminal)
		}
	}

	for _, name := range previewTerminalCandidates(previewTerminal, os.Getenv("TERMINAL")) {
		if _, err := exec.LookPath(name); err != nil {
			continue
		}

		args := argsForTerminal(name, shellArgs)
		cmd := exec.Command(name, args...)
		cmd.Env = env

		if err := startTerminal(cmd); err == nil {
			color.White("Opening preview in %s (theme: %s)...", name, themeName)
			return nil
		}
	}

	return manualPreviewFallback(starshipConfig, shell)
}

// shellQuoteUnix wraps s in single quotes for a POSIX shell, escaping any
// embedded single quote using the standard POSIX technique (close the
// quote, emit a backslash-escaped quote, reopen the quote), since
// single-quoted strings admit no escape mechanism of their own. Used so a
// theme path containing spaces or shell metacharacters still round-trips
// correctly through the manual fallback command.
func shellQuoteUnix(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// psSingleQuote wraps s in single quotes for PowerShell, escaping an
// embedded single quote by doubling it - PowerShell's single-quoted-string
// escape rule (there is no backslash escaping inside single quotes there).
func psSingleQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

// windowsManualCommand builds the PowerShell one-liner shown by
// manualPreviewFallback on Windows. It launches a NEW pwsh/powershell
// process with STARSHIP_CONFIG set only inside that process (-NoExit keeps
// it open for the actual preview) rather than mutating the current
// session's environment permanently, which `$env:STARSHIP_CONFIG=...;
// powershell` (the old fallback) did. pwsh (PowerShell 7+) is preferred when
// it's on PATH; Windows PowerShell (powershell.exe), always present, is the
// fallback. lookPath is injected so tests can fake PATH contents.
func windowsManualCommand(starshipConfig string, lookPath func(string) (string, error)) string {
	psExe := "powershell"
	if _, err := lookPath("pwsh"); err == nil {
		psExe = "pwsh"
	}
	return fmt.Sprintf(`%s -NoExit -Command "$env:STARSHIP_CONFIG=%s"`, psExe, psSingleQuote(starshipConfig))
}

// unixManualCommand builds the shell one-liner shown by
// manualPreviewFallback everywhere but Windows: STARSHIP_CONFIG set for a
// single command, scoped to the spawned shell only.
func unixManualCommand(starshipConfig, shell string) string {
	return fmt.Sprintf("STARSHIP_CONFIG=%s %s", shellQuoteUnix(starshipConfig), shell)
}

// manualPreviewFallback tells the user the exact command to preview the
// theme in their current terminal when stellar could not spawn a new window
// itself - no terminal emulator was found, none of the candidates could
// actually be started, or (on Windows, and any other unhandled platform)
// spawning one isn't implemented at all. It never claims a preview happened:
// it hands the user a command to run themselves, rather than overclaiming
// "Preview opened" when nothing was.
func manualPreviewFallback(starshipConfig, shell string) error {
	color.Yellow("Could not open a new terminal window automatically.")
	color.Cyan("Preview it manually by running this in your current terminal:")
	if runtime.GOOS == "windows" {
		fmt.Printf("  %s\n", windowsManualCommand(starshipConfig, exec.LookPath))
	} else {
		fmt.Printf("  %s\n", unixManualCommand(starshipConfig, shell))
	}
	return nil
}

// cmd/preview.go
package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"

	"github.com/a3chron/stellar/internal/api"
	"github.com/a3chron/stellar/internal/cache"
	"github.com/a3chron/stellar/internal/theme"
	"github.com/fatih/color"
	"github.com/spf13/cobra"
)

var previewCmd = &cobra.Command{
	Use:   "preview [author/theme[@version]]",
	Short: "Preview a theme in a new terminal window",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		identifier := args[0]

		// Parse and download if needed
		t, err := theme.ParseIdentifier(identifier)
		if err != nil {
			return err
		}

		if !cache.ThemeExists(t) {
			color.Yellow("Downloading %s...", t)
			client := api.NewClient()
			content, err := client.FetchThemeConfig(t.Author, t.Name, t.Version)
			if err != nil {
				return err
			}
			validationResult, err := theme.ValidateConfigContent(content)
			if err != nil {
				return err
			}
			if !validationResult.Valid {
				return validationResult.Error
			}
			if err := cache.SaveTheme(t, content); err != nil {
				return err
			}
		}

		themePath, err := t.CachePath()
		if err != nil {
			return err
		}

		// Spawn terminal
		err = spawnTerminalWithEnv(themePath, t.String())
		if err != nil {
			return err
		}

		color.Green("\nPreview opened in new window!")
		color.Cyan("Theme: %s", t)

		return nil
	},
}

func spawnTerminalWithEnv(starshipConfig, themeName string) error {
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/bash"
	}

	switch runtime.GOOS {
	case "darwin": // macOS
		return spawnMacTerminal(starshipConfig, shell)
	case "linux":
		return spawnLinuxTerminal(starshipConfig, themeName, shell)
	default:
		return fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}
}

func spawnMacTerminal(starshipConfig, shell string) error {
	termProgram := os.Getenv("TERM_PROGRAM")

	var script string
	if termProgram == "iTerm.app" {
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
	return cmd.Start()
}

func spawnLinuxTerminal(starshipConfig, themeName, shell string) error {
	type terminal struct {
		name string
		args func(shellArgs []string) []string
	}

	terminals := []terminal{
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

	// Prefer fish → zsh → provided shell
	previewShell := shell
	if _, err := exec.LookPath("fish"); err == nil {
		previewShell = "fish"
		color.White("Using fish for preview")
	} else if _, err := exec.LookPath("zsh"); err == nil {
		previewShell = "zsh"
		color.White("Using zsh for preview")
	}

	shellArgs := func(sh string) []string {
		switch sh {
		case "fish":
			return []string{"fish", "-l"}
		case "zsh":
			return []string{"zsh", "-l"}
		case "bash":
			return []string{"bash", "-l"}
		default:
			return []string{sh}
		}
	}(previewShell)

	env := os.Environ()
	env = append(env,
		fmt.Sprintf("STARSHIP_CONFIG=%s", starshipConfig),
		"STARSHIP_LOG=error",
		"STARSHIP_CACHE=/tmp/starship-preview",
	)

	for _, term := range terminals {
		if _, err := exec.LookPath(term.name); err != nil {
			continue
		}

		args := term.args(shellArgs)
		cmd := exec.Command(term.name, args...)
		cmd.Env = env

		color.White("Launching %s with theme: %s", term.name, themeName)

		if err := cmd.Start(); err == nil {
			return nil
		}
	}

	return fmt.Errorf("no supported terminal could be launched")
}

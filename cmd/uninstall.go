package cmd

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/a3chron/stellar/internal/config"
	"github.com/a3chron/stellar/internal/paths"
	"github.com/a3chron/stellar/internal/symlink"
	"github.com/fatih/color"
	"github.com/spf13/cobra"
)

var (
	uninstallYes        bool
	uninstallKeepConfig bool
)

// executablePath resolves the running binary. A package var so the E2E test
// can point it at a scratch file instead of letting the command delete the
// go-test binary it is running inside.
var executablePath = os.Executable

var uninstallCmd = &cobra.Command{
	Use:   "uninstall",
	Short: "Remove stellar from this machine",
	Long: `Remove stellar from this machine.

This detaches your prompt first: if starship.toml is a symlink into stellar's
cache, it is replaced by a plain copy of the theme you have applied, so your
prompt keeps working after the cache is gone. Then the stellar directory
(cache, config and the backups of your original starship.toml) and the
stellar binary itself are removed.

Use --keep-config to keep ~/.config/stellar (themes, backups, config) and
remove only the binary.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		execPath, err := executablePath()
		if err != nil {
			return fmt.Errorf("failed to locate the stellar binary: %w", err)
		}
		if resolved, rerr := filepath.EvalSymlinks(execPath); rerr == nil {
			execPath = resolved
		}

		stellarHome, err := paths.StellarHome()
		if err != nil {
			return err
		}
		starshipPath, err := symlink.StarshipConfigPath()
		if err != nil {
			return err
		}

		cfg, err := config.Load()
		if err != nil {
			cfg = &config.Config{}
		}

		detachTarget := managedSymlinkTarget(starshipPath, stellarHome)

		// Say exactly what is about to happen before asking - this is the one
		// stellar command that deletes things the user may not be able to get
		// back (their original starship.toml backups).
		color.Yellow("This will:")
		if detachTarget != "" {
			fmt.Printf("  - replace the symlink %s with a plain copy of your current theme\n", starshipPath)
		}
		if uninstallKeepConfig {
			fmt.Printf("  - keep %s (themes, backups, config)\n", stellarHome)
		} else {
			fmt.Printf("  - remove %s (cached themes, config AND the backups of your original starship.toml)\n", stellarHome)
		}
		fmt.Printf("  - remove the binary %s\n", execPath)

		if !uninstallYes {
			ok, err := confirmUninstall(cmd.InOrStdin())
			if err != nil {
				return err
			}
			if !ok {
				fmt.Println("Aborted, nothing was changed.")
				return nil
			}
		}

		// The hub is told first, while config.json (and the install id in it)
		// still exists. Whatever the PreRun report was doing is dropped: it
		// would otherwise re-create config.json after the directory is gone.
		cancelTelemetry()
		if reportUninstall(cfg) {
			fmt.Println("Told the hub this install is gone.")
		}

		if detachTarget != "" {
			if err := detachPrompt(starshipPath, detachTarget); err != nil {
				return fmt.Errorf("failed to detach %s from the stellar cache: %w", starshipPath, err)
			}
			fmt.Printf("Detached %s (now a regular file).\n", starshipPath)
		}

		if !uninstallKeepConfig {
			if err := os.RemoveAll(stellarHome); err != nil {
				return fmt.Errorf("failed to remove %s: %w", stellarHome, err)
			}
			fmt.Printf("Removed %s.\n", stellarHome)
		}

		if leftover := removeSelf(execPath); leftover != "" {
			color.Yellow("Could not delete the binary itself. Remove it by hand:")
			fmt.Printf("  %s\n", leftover)
		} else {
			fmt.Printf("Removed %s.\n", execPath)
		}

		color.Green("stellar has been uninstalled. Thanks for trying it!")
		if runtime.GOOS == "windows" {
			fmt.Println("The install directory is still on your user PATH; the uninstall.ps1 script removes that entry.")
		}
		return nil
	},
}

// confirmUninstall reads a yes/no answer from in. When in is a file that is
// not a terminal (stdin as a pipe, CI, a script), there is nobody to answer,
// so the command refuses rather than hanging or guessing.
func confirmUninstall(in io.Reader) (bool, error) {
	if f, ok := in.(*os.File); ok {
		info, err := f.Stat()
		if err != nil || info.Mode()&os.ModeCharDevice == 0 {
			return false, fmt.Errorf("stdin is not a terminal; re-run with --yes to uninstall non-interactively")
		}
	}

	fmt.Print("Continue? [y/N] ")
	line, err := bufio.NewReader(in).ReadString('\n')
	if err != nil && err != io.EOF {
		return false, err
	}
	answer := strings.ToLower(strings.TrimSpace(line))
	return answer == "y" || answer == "yes", nil
}

// managedSymlinkTarget returns the file starshipPath links to when it is a
// symlink pointing into stellarHome, and "" otherwise (a regular file - copy
// mode or an unmanaged config - or a link to somewhere stellar does not own).
// Only a link INTO the cache needs detaching, because only that breaks when
// the cache is removed.
func managedSymlinkTarget(starshipPath, stellarHome string) string {
	info, err := os.Lstat(starshipPath)
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		return ""
	}
	target, err := os.Readlink(starshipPath)
	if err != nil {
		return ""
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(filepath.Dir(starshipPath), target)
	}
	target = filepath.Clean(target)

	home := filepath.Clean(stellarHome)
	if resolved, err := filepath.EvalSymlinks(home); err == nil {
		home = resolved
	}
	resolvedTarget := target
	if r, err := filepath.EvalSymlinks(target); err == nil {
		resolvedTarget = r
	}

	within := func(p string) bool {
		return p == home || strings.HasPrefix(p, home+string(filepath.Separator))
	}
	if within(target) || within(resolvedTarget) {
		return target
	}
	return ""
}

// detachPrompt replaces the symlink at starshipPath with a regular file holding
// target's content. Written to a temp sibling and renamed into place, like
// ApplyTheme does, so a failure can never leave the prompt with no config.
func detachPrompt(starshipPath, target string) error {
	content, err := os.ReadFile(target)
	if err != nil {
		return err
	}
	tempPath := filepath.Join(filepath.Dir(starshipPath), ".starship.toml.stellar-detach")
	_ = os.Remove(tempPath)
	if err := os.WriteFile(tempPath, content, 0644); err != nil {
		return err
	}
	if err := symlink.RenameWithRetry(tempPath, starshipPath); err != nil {
		_ = os.Remove(tempPath)
		return err
	}
	return nil
}

// removeSelf deletes the running binary. On Unix an executing file can simply
// be unlinked. Windows refuses to delete a running .exe but allows renaming
// it, so the binary is moved aside to ".old" (the same slot "stellar update"
// uses) and a detached cmd.exe deletes it once this process has exited. It
// returns the path the user must delete by hand if that fails, "" on success.
func removeSelf(execPath string) (leftover string) {
	if runtime.GOOS != "windows" {
		if err := os.Remove(execPath); err != nil {
			return execPath
		}
		return ""
	}

	oldPath := execPath + ".old"
	_ = os.Remove(oldPath)
	if err := symlink.RenameWithRetry(execPath, oldPath); err != nil {
		return execPath
	}

	// "timeout" waits for this process to release the file; "del" then
	// removes it. Start (not Run): the whole point is to outlive us.
	script := fmt.Sprintf(`timeout /t 2 /nobreak >nul & del /f /q "%s"`, oldPath)
	deleter := exec.Command("cmd", "/C", script)
	if err := deleter.Start(); err != nil {
		return oldPath
	}
	return ""
}

func init() {
	uninstallCmd.Flags().BoolVarP(&uninstallYes, "yes", "y", false, "Skip the confirmation prompt")
	uninstallCmd.Flags().BoolVar(&uninstallKeepConfig, "keep-config", false, "Keep ~/.config/stellar (themes, backups, config); remove only the binary")
}

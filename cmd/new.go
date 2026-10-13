// cmd/new.go implements `stellar new`: a brand-new, empty 1.0 theme ready to
// edit, apply, and later publish with `stellar publish`.
package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/a3chron/stellar/internal/config"
	"github.com/a3chron/stellar/internal/paths"
	"github.com/fatih/color"
	"github.com/spf13/cobra"
)

// starterConfigTemplate is the small 1.0 config `stellar new` writes - just
// enough to be a valid, useful starting point rather than an empty file.
const starterConfigTemplate = `# %s - a starship theme
# Edit this file, then run "stellar apply %s" to try it out.

format = "$all"

[character]
success_symbol = "[➜](bold green)"
error_symbol = "[➜](bold red)"
`

var newCmd = &cobra.Command{
	Use:   "new [author/]name",
	Short: "Create a new, empty theme",
	Long: `Create a new starship theme under your stellar cache, ready to edit, apply,
and later publish with "stellar publish".

Without an author prefix, the theme is created under your logged-in username
(or your OS username if you haven't run "stellar auth" yet).`,
	Example: `  stellar new my-theme
  stellar new a3chron/my-theme`,
	Args: argsWithUsage(cobra.ExactArgs(1)),
	RunE: func(cmd *cobra.Command, args []string) error {
		author, name, err := splitAuthoredName(args[0])
		if err != nil {
			return err
		}

		path, err := paths.ThemeCachePath(author, name, "1.0")
		if err != nil {
			return err
		}

		if err := ensureFreshThemeDir(author, name, filepath.Dir(path)); err != nil {
			return err
		}

		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			return err
		}

		identifier := fmt.Sprintf("%s/%s", author, name)
		content := fmt.Sprintf(starterConfigTemplate, name, identifier)
		if err := writeNewThemeFile(path, []byte(content)); err != nil {
			return err
		}

		cfg, err := config.Load()
		if err != nil {
			cfg = &config.Config{}
		}
		cfg.MarkAuthored(identifier)
		if err := cfg.Save(); err != nil {
			color.Yellow("Warning: couldn't record %s as authored (stellar clean may remove it): %v", identifier, err)
		}

		color.Green("Created %s", identifier)
		fmt.Printf("  %s\n", path)
		fmt.Println()
		color.Cyan("Next steps:")
		fmt.Printf("  stellar apply %s      # try it out\n", identifier)
		fmt.Printf("  stellar publish %s    # publish it to stellar-hub\n", identifier)
		return nil
	},
}

func init() {
	newCmd.ValidArgsFunction = cobra.NoFileCompletions
}

# stellar-cli
Easily get and switch between starship configs

![stellar cli demo](assets/demo.gif)
<table>
  <tbody>
    <tr>
      <td><img src="assets/web-hub.png" alt="web hub preview" /></td>
      <td><img src="assets/web-hub-theme.png" alt="web hub theme detail" /></td>
    </tr>
  </tbody>
</table>


## Installation

### Linux / macOS

Just run the [install script](https://raw.githubusercontent.com/a3chron/stellar/main/install.sh) 
(which will download the binary and move it to `~/.local/bin`)
```bash
curl -fsSL https://raw.githubusercontent.com/a3chron/stellar/main/install.sh | bash
```

### Windows

Run the [PowerShell install script](https://raw.githubusercontent.com/a3chron/stellar/main/install.ps1) 
(which downloads the binary to `%LOCALAPPDATA%\stellar\bin` and adds it to your PATH)
```powershell
irm https://raw.githubusercontent.com/a3chron/stellar/main/install.ps1 | iex
```

Check that stellar is installed with `stellar --version` or `stellar --help`, 
and search for a theme you like on the [stellar hub](https://stellar.a3chron.dev) to apply.

For just switching between your own local configs, check out the [local configs](#local-configs) section.

Some [basic usage](#basic-usage) covered here, for more info, run `stellar --help`

<span id="windows" />

> [!NOTE]
> On Linux and macOS, stellar applies a theme by **symlinking** `~/.config/starship.toml` to the cached
> config, which gives you hot-reload while editing local configs.
>
> Windows is bad at symlinks (they need Developer Mode or admin privileges), so on Windows stellar
> **copies** the theme file over `starship.toml` instead. Everything works the same, except
> editing a local theme file, it does **not** live-update `starship.toml` (it's a copy, not a link). Just
> re-run `stellar apply <author>/<theme>` after editing.
>
> You can force copy mode anywhere (e.g. for testing) by setting `STELLAR_APPLY_MODE=copy`.

### Shell completion

stellar ships tab completion for commands, flags, and theme identifiers
(`author/slug@version`). Candidates come from your local theme cache first, so
completing a theme you already have is instant and works offline.

If nothing in your cache matches what you typed, stellar falls back to the
stellar-hub and suggests themes from there, so you can tab-complete a theme
you've never downloaded. The fallback is bounded at 800ms and degrades
silently: offline, or on a slow hub, you simply get the local candidates (and
never an error or a hung terminal). Hub candidates are marked `hub`, cached
ones `local`.

Tune it with `STELLAR_COMPLETION_ONLINE`:

| value | behaviour |
| --- | --- |
| unset (default) | local cache, falling back to the hub only when the cache has no match |
| `1` / `true` / `yes` / `on` | also merge in hub themes when the cache *did* match (costs a hub round trip on every completion, up to 2s) |
| any other value | local cache only, never any network |

Anything set to an unrecognised *value* (`0`, `false`, `off`, `no`, a typo)
means local only - set it to turn the network off and it turns off. Setting it
to an empty string counts as leaving it unset, and keeps the default.

`stellar remove` always completes from the local cache only - it can only
remove themes you actually have.

```bash
# bash
stellar completion bash > ~/.local/share/bash-completion/completions/stellar

# zsh
stellar completion zsh > "${fpath[1]}/_stellar"

# fish
stellar completion fish > ~/.config/fish/completions/stellar.fish

# powershell (add to $PROFILE)
stellar completion powershell | Out-String | Invoke-Expression
```

### Uninstall

```bash
# Linux / macOS
curl -fsSL https://raw.githubusercontent.com/a3chron/stellar/main/uninstall.sh | bash
```
```powershell
# Windows (also removes the install dir from your PATH)
irm https://raw.githubusercontent.com/a3chron/stellar/main/uninstall.ps1 | iex
```

Both scripts run `stellar uninstall --yes`, which you can also call yourself:

```bash
stellar uninstall                # asks for confirmation first
stellar uninstall --yes          # no questions asked
stellar uninstall --keep-config  # remove only the binary, keep ~/.config/stellar
```

Uninstalling detaches your prompt first: if `starship.toml` is a symlink into
stellar's cache, it is replaced by a plain copy of the theme you have applied,
so starship keeps working. Then `~/.config/stellar` (cached themes, config and
the [backups of your original config](#automatic-backup-of-your-original-config))
and the binary are removed. Restore a backup with `stellar apply <username>/backup`
*before* uninstalling if you want your original prompt back, or pass
`--keep-config` to keep the directory.

## Why use

**Before:** Getting good starship configs so far was mostly random, from someones github dotfiles, searching for something entirely else...  


**With stellar:** Find the right theme on the [stellar hub](https://stellar.a3chron.dev) & `stellar apply <author>/<theme>`.

### Usecases

There are a few usecases for stellar:
- You want to switch your starship prompt / theme from time to time (without manually copying starship configs)
- You want to try a few different community prompts
- You are working on a theme, and need to switch around between your normal and development version often
- You have a script to change the theme of the whole system / terminal in some kind, including the starship prompt

## Basic Usage

```bash
# Apply a theme / config (uses latest local version or downloads latest version, e.g., 1.2.toml)
stellar apply a3chron/ctp-blue

# Apply a specific version
stellar apply a3chron/ctp-blue@1.2

# Check for updates and download if available
stellar apply a3chron/ctp-blue --update

# Preview before applying (will open an extra window)
stellar preview a3chron/ctp-red

# List cached themes
stellar list

# Show current theme
stellar current

# Get theme info
stellar info a3chron/ctp-green

# Clean cache (keep current)
stellar clean

# Remove all versions of a theme
stellar remove a3chron/ctp-green

# Remove specific version only
stellar remove a3chron/ctp-green@1.0

# Rollback to previous theme
stellar rollback

# Update CLI
stellar update

# Remove stellar from this machine (see Uninstall above)
stellar uninstall
```

### Confirmations and `--force`

`apply`, `preview` and `rollback` all show a security warning and ask
`Do you trust this theme and want to apply/preview/restore it? [y/N]:`
before ever running starship against a theme that contains `[custom]`
commands (those can execute arbitrary shell code on every prompt render).
This only ever fires for genuinely new content: a theme that's already
cached locally is never re-prompted, only a fresh download from the hub is
(rollback's re-download-when-missing path included).

- `y`/`yes` (however it reaches stdin - typed, piped, or redirected)
  confirms; anything else declines and the command aborts with a non-zero
  exit.
- If stdin hits end-of-file with nothing typed at all and isn't a real
  terminal (`stellar apply ... </dev/null`, a detached CI stdin, a script
  that doesn't pipe an answer in), the command fails with
  `no answer on stdin; re-run with --force to skip this confirmation`
  instead of silently treating that as "no". `echo y | stellar apply ...`
  and terminals that don't look like a TTY (e.g. mintty/Git Bash on
  Windows) still work fine, since they do have a real answer to read.

Pass `--force` (`-f`) to `apply`, `preview` or `rollback` to skip that
confirmation outright. `--force` also has a second effect: it skips TOML
validation (and the 100KB size cap) for a theme that's **already on
disk** - cached, local/hand-written, or (for `rollback`) the previous
theme - printing a yellow warning instead of refusing, so you can force
through your own work-in-progress config. A **freshly downloaded** theme
is always validated in full regardless of `--force`; only the
`[custom]`-command confirmation is skippable for that content.

`stellar remove` also has `--force`, with an unrelated meaning: it lets you
remove the currently active theme (normally refused).

### `--shell` and `--terminal` (preview)

`stellar preview` opens a new terminal window running the theme with
`STARSHIP_CONFIG` pointed at it, without touching your real config.

- `--shell` picks the shell run inside that window (defaults to `$SHELL`,
  falling back to `fish`, then `zsh`, then `bash`, then a bare `sh`).
- `--terminal` picks the terminal emulator to spawn on Linux (defaults to
  `$TERMINAL`, falling back to a list of common terminals: wezterm,
  alacritty, ghostty, kitty, foot, kgx, gnome-terminal, tilix, konsole,
  xfce4-terminal, xterm). Matching works even if `--terminal`/`$TERMINAL`
  is a full path (e.g. `/usr/bin/wezterm`). If `--terminal` is given but
  not found on `PATH`, stellar prints a warning and falls back to
  `$TERMINAL`/the known list instead of just failing.
  **`--terminal` is ignored on macOS**, which always opens Terminal.app
  (or iTerm, if `$TERM_PROGRAM` says you're running it).

If no terminal could be opened at all (none found, none of them would
start, or on an unsupported platform), preview never claims a window was
opened - it prints the exact command to run manually instead, with the
theme path properly shell-quoted (or, on Windows, a `pwsh`/`powershell`
one-liner that only affects the newly spawned process, not your current
session).

### Stellar Hub

You can see all available community themes at the [stellar hub](https://stellar.a3chron.dev).

#### Publishing Your Themes

I am working on getting a `stellar publish` command, but currently you will have to publish your theme at [the upload form](https://stellar.a3chron.dev/upload).

If you want to update a theme, you can do so in your stellar hub settings, either "Edit Metadata" (The pencil icon), or "Update" (The upload icon), 
with beeing able to update either metadata like the theme name, description, prerequesites etc., or upload a new config version with version notes.

## Local configs

### Automatic backup of your original config

When you first use `stellar apply`, if you have an existing `~/.config/starship.toml` that's not managed by stellar, it will be automatically backed up to `~/.config/stellar/<username>/backup/1.0.toml` before creating the symlink.

Backups are versioned. If stellar later finds another unmanaged `starship.toml` (for example one you restored or hand-wrote), it backs that up too as `2.0.toml`, then `3.0.toml`, and so on, so an earlier backup is never overwritten.

This also works in copy mode (the Windows default): if you edit the applied `starship.toml` directly, stellar notices the file no longer matches the theme it applied and backs up your edits before applying the next theme.

If `~/.config/starship.toml` is itself a symlink stellar didn't create - for example one managed by a dotfiles tool like stow, chezmoi, or home-manager - stellar does **not** silently adopt it. The symlink's target content is backed up exactly like an unmanaged regular file, and `stellar apply` tells you plainly what happened and how to get it back, e.g.:

```
~/.config/starship.toml was a symlink to ~/dotfiles/starship.toml.
Its content was backed up as <username>/backup@1.0 - stellar now manages this path.
```

Only a symlink stellar itself created (pointing into `~/.config/stellar/`) is ever treated as already managed - a foreign symlink is never adopted silently, even if its target happens to contain the exact same bytes as the theme you last applied.

Tools like home-manager or stow re-create that exact same foreign symlink on every activation, so re-running `stellar apply` afterward would otherwise mint a pointless new backup version (`2.0.toml`, `3.0.toml`, ...) every time, for content that never actually changed. Stellar checks the new content against the newest backup already on disk first, and if it's byte-identical, reuses that backup instead of creating another one - it still tells you what was replaced, just without claiming a fresh backup was made:

```
~/.config/starship.toml was a symlink to ~/dotfiles/starship.toml.
Its content is already backed up as <username>/backup@1.0 - stellar now manages this path.
```

Stellar recognizes its own applied file by a checksum recorded in `~/.config/stellar/config.json`, independent of apply mode (symlink or copy) or OS. This means editing a cached theme file directly and re-applying it, or running `stellar clean`/`stellar clean --all` and then applying another theme, never creates a spurious backup, only a config file you actually hand-edited yourself gets preserved.

`stellar clean` (with or without `--all`) never deletes your backups either. 
They're preserved automatically and only removed if you explicitly run `stellar remove <username>/backup`.

This ensures your carefully crafted config is never lost :) You can apply the newest backup anytime with:
```bash
stellar apply <username>/backup
```

To restore a specific one, for instance your very first original config, just pin the version:
```bash
stellar apply <username>/backup@1.0
```

You can also rename the backup folder to give it a proper theme name:
```bash
mv ~/.config/stellar/<username>/backup ~/.config/stellar/<username>/my-custom-theme
stellar apply <username>/my-custom-theme
```

### Switching between local configs

You can just put your own configs under `~/.config/stellar/<your-username>/<your-theme>/1.0.toml`,
and then switch to them using `stellar apply <your-username>/<your-theme>`.

> [!NOTE]
> The `/<your-username>` is not needed, you can actually use whatever you would like, i.e. `/local`, `/dev` or similar,
> including existing usernames (like yours, if you also publish themes), just create an extra folder for your theme

### Customizing themes

You can similarily copy one existing downloaded theme to the `stellar/<your-username>` folder, edit it,
and then switch to it using `stellar apply ...`.

> [!NOTE]
> @ here again, you don't need `/<your-username>`, so you can theoretically just copy for example
> `a3chron/ctp-red/1.0.toml` to `a3chron/dev/1.0.toml` or any other folder name

Because stellar is using a symlink to the currently selected config file, you get hot-reload as well for editing configs, just like with the usual `starship.toml`.

> [!NOTE]
> On Windows stellar copies the config instead of symlinking it (see the [Windows note](#windows)),
> so editing a local theme file does **not** hot-reload, you'll have to re-run `stellar apply <author>/<theme>` after editing.

## Troubleshooting

`apply`, `preview` and `info` all resolve a theme against stellar-hub first and share the exact same wording for what happens when that doesn't go cleanly, so the messages below show up under any of the three.

### "Theme not found online, using local cache" / "Can't reach stellar-hub (are you offline?), using local cache" / "No theme author/theme on stellar-hub, using local cache"

One of these prints when stellar can't get a version from stellar-hub but a local cache exists to fall back to - the exact wording depends on why:

- **No internet connection** ("are you offline?") - stellar will use your locally cached version of the theme
- **Theme was deleted from the hub** ("no theme ... on stellar-hub") - if you previously downloaded it, your local copy still works
- **Theme was renamed in the hub** - if you previously downloaded it, your local copy still works, you can search for the theme in the hub going to `/<username>` in the hub (usernames cannot be changed (yet))
- **Local-only theme** - if you created the theme manually in `~/.config/stellar/`, this is expected behavior

This is usually not a problem - stellar will use whatever version you have cached locally. `stellar info` shows the same idea with `(offline - showing cached info only)`, or `(not on stellar-hub - showing local copy)` when the theme simply isn't published (a 404, as opposed to being unreachable).

### "no theme author/theme on stellar-hub - is that the right theme name?" / "no author author on stellar-hub" / "theme not found: author/theme (not available online and no local cache)" / "can't reach stellar-hub (are you offline?) and no local cache for author/theme"

One of these errors (non-zero exit) means the theme couldn't be resolved at all - no cache to fall back to either. This is the same error whether or not you gave an explicit `@version` - a typo'd theme name is reported as a missing theme either way, never as a missing version:
- Check if you typed the theme name correctly
- The theme may have been removed from stellar-hub
- You may be offline, with nothing cached locally for this theme yet
- For local themes, make sure you created the folder at `~/.config/stellar/<author>/<theme>/` with a `.toml` file

"no author author on stellar-hub" specifically means nobody has published anything under that author handle - most often a typo in the author, not the theme name.

When something close enough exists, either of these errors is followed by a `Did you mean: ...` line (or several, one per line, ranked closest first) suggesting a theme from the same author, a matching slug published under a different author, or something already in your local cache. It's a best-effort suggestion (skipped if nothing is close, or if stellar-hub can't be reached quickly enough) - never assume the suggestion is correct, but it usually is.

### "author/theme has no version X - is that the right version?"

The theme itself exists, but not the version you asked for. The error lists the versions that do exist (oldest first, with the latest called out), and - unless nothing published is remotely close - a `Did you mean: stellar apply/preview/info author/theme@<version>` line suggesting the closest one (same major version, nearest minor, or the latest otherwise). If stellar-hub can't be reached but this theme has other versions cached locally, those are used instead of a generic "offline" error - they're real evidence of what versions actually exist.

### "aborted: theme was not applied/previewed/restored" / "no answer on stdin; re-run with --force to skip this confirmation"

These come from the `[custom]`-command confirmation prompt (see [Confirmations and `--force`](#confirmations-and---force)) - either you (or a script) answered anything other than `y`/`yes`, or stdin had no answer to give at all and isn't a real terminal. Pass `--force` to skip the prompt.

### Exit codes

`stellar remove` and `stellar rollback` exit non-zero on a refusal, not just a crash:
- `stellar remove` with several identifiers removes as many as it can and still exits `1` if any of them failed (already-active theme without `--force`, not in your local cache, ...) - the successful removals are not rolled back.
- `stellar rollback` exits `1` when there's nothing to roll back to: no previous theme recorded yet, or previous and current are the same theme (this happens after re-applying the same theme with an older stellar version; applying a different theme fixes it going forward).

## Telemetry

stellar sends an anonymous ping on install, update and uninstall (a random id,
the CLI version and your OS) so the hub can count installs - no themes, paths
or IPs. Opt out with `STELLAR_NO_TELEMETRY=1` or `DO_NOT_TRACK=1`; details are
in the [privacy policy](https://stellar.a3chron.dev/legal#privacy).

## Contributing

All contributions are welcome :)  
The easiest way to contribute is to [upload your own starship config](https://stellar.a3chron.dev/upload) for other to use ;)

Please use [conventional commits](https://www.conventionalcommits.org/) for PRs,
and check for lint errors with `golangci-lint run` (included in the flake).

### Cutting a release

```bash
nix develop --command ./scripts/release.sh 1.5.0
```

The script tags the release, pushes the tag (which is what triggers goreleaser),
then updates `nix/package.nix` to match and pushes that.

Those two values - `version` and the source `hash` - have to move together, and cannot be collapsed into a single edit: the hash is a
content hash of the *tagged* tarball, so it does not exist until the tag is
pushed. That fixed ordering is the whole reason the script exists. It refuses to
run on a dirty tree, off `main`, out of sync with origin, or when the tag
already exists, and `--dry-run` shows what it would do.

`flake.nix` has nothing to bump - it derives its version from the commit,
because it builds the working tree rather than a release.

### vhs

To record a vhs video just run:
```bash
vhs demo.tape
```

For nix users:
```bash
nix-shell -p vhs
```

### Testing

#### Running Tests

From the `stellar-cli` directory:

```bash
# Enter development environment (NixOS)
nix develop

# Run E2E tests (recommended - tests all CLI functionality)
./run-tests.sh -e

# Run E2E tests against production API
./run-tests.sh -ep

# Run unit tests (internal modules)
./run-tests.sh -u

# Run all tests
./run-tests.sh -a

# Interactive menu
./run-tests.sh
```

#### Test Types

- **E2E tests**: Test complete user workflows (apply, remove, list, etc.). These are the primary tests and cover all CLI functionality.
- **Unit tests**: Test internal modules (API client, theme parser). Less important, mainly for edge cases.

#### Contributing Tests

When adding new CLI features, please add corresponding E2E tests in `cmd/e2e_test.go`. This ensures the feature works from a user's perspective. Unit tests are not needed unless the feature has complex internal logic.

## TODOs

- [x] Allow removing several themes at once: `stellar remove a3chron/ctp-green a3chron/ctp-red`
- [x] Preview: maybe cache in /tmp, os not downloading two times, but also not saving previewed themes in stellar cache
- [x] Add tests
- [x] **Windows support**: apply themes by copying instead of symlinking (`STELLAR_APPLY_MODE`), Windows release binary, PowerShell installer, and `stellar update`

- [x] **Preview: fix bash formatting**: the literal `\[ \]` and `shopt: progcomp` errors came from `nix develop` dropping into the readline-less stdenv bash; the dev shell now uses `bashInteractive`, and `stellar preview --shell bash` opens a normal bash
- [ ] **`stellar preview` on Windows**: `cmd/preview.go` only spawns terminals on macOS/Linux. On Windows (and anywhere else no terminal could be opened) it now prints the exact command to preview manually instead of erroring, but still doesn't open a window itself - needs a Windows Terminal / PowerShell branch that opens a shell with `STARSHIP_CONFIG` set.
- [ ] **Windows packaging**: consider scoop/winget packaging (leftover `stellar.exe.old` from self-update is already cleaned up on the next run).
- [x] **CI test job**: `go vet` and `go test` (with `-race` on Linux) run on every pull request and again before goreleaser, on both `ubuntu-latest` and `windows-latest`, so a tag can't publish a failing build and the copy path is guarded natively.
- [ ] **`stellar publish` command**: Upload local themes directly to stellar-hub
  - Challenge: Need to implement CLI authentication (OAuth flow with browser redirect or API keys)
  - Would read from `~/.config/stellar/<author>/<theme>/<version>.toml`
  - Interactive prompts for metadata (name, description, screenshot, etc.)
  - Skip complex fields initially (e.g., color scheme selection - add later)
- [ ] **`stellar update <theme>` command**: Update an existing theme on stellar-hub with a new version
  - Requires authentication (same challenge as publish)
  - Upload new version of already published theme
  - Interactive prompts for version notes, dependencies, etc.
- [ ] Add progress bars for downloads
- [ ] **Get stellar into nixpkgs**: the flake is nixified - `nix build` and `nix run` produce a
  real binary with version ldflags and installed bash/zsh/fish completions, and
  `nix/package.nix` is a release build pinned to a tag, ready to be copied into nixpkgs as
  `pkgs/by-name/st/stellar/package.nix`. Remaining for the PR: add a maintainer entry to
  `maintainers/maintainer-list.nix` and fill in `meta.maintainers`.

<br />

<p align="center"><a href="https://github.com/a3chron/stellar/blob/main/LICENSE"><img alt="GitHub License" src="https://img.shields.io/github/license/a3chron/stellar?style=for-the-badge&labelColor=363a4f&color=b7bdf8">
</a></p>

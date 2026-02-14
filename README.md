# stellar-cli
Easily get and switch between starship configs

![stellar cli demo](assets/demo.gif)
![stellar hub](assets/web-hub.png)

## Installation

Just run the [install script](https://raw.githubusercontent.com/a3chron/stellar/main/install.sh) 
which will download the binary and move it to `~/.local/bin`
```bash
curl -fsSL https://raw.githubusercontent.com/a3chron/stellar/main/install.sh | bash
```
<a >

> [!WARNING]
> stellar is not yet available for windows, because stellar uses symlinks, and windows is weird with symlinks.  
> Windows users would need to either:
> - Run it with admin privileges
> - Enable Developer Mode in Windows 10+
> - (Use WSL -> not really "Windows")
> 
> which is not optimal, and means we will have to do a special case for windows, which may take some time

## Why use

**Before:** Getting good starship configs so far was mostly random, from someones github dotfiles, searching for something entirely else...  


**With stellar:** Find the right theme on the [stellar hub](https://stellar-hub.vercel.app) & `stellar apply <author>/<theme>`.

### Usecases

There are a few usecases for stellar:
- You want to switch your starship prompt / theme from time to time (without manually copying starship configs)
- You want to try a few different community prompts
- You are working on a theme, and need to switch around between you normal and development version often
- You have a script to change the theme of the whole system / terminal in some kind, including the starship prompt

## Basic Usage

```bash
# Apply a theme / config
stellar apply a3chron/ctp-blue
stellar apply a3chron/ctp-blue@1.2

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

# Remove specific theme
stellar remove a3chron/ctp-green

# Rollback to previous theme
stellar rollback

# Update CLI
stellar update
```

## Local configs

### Automatic backup of your original config

When you first use `stellar apply`, if you have an existing `~/.config/starship.toml` that's not managed by stellar, it will be automatically backed up to `~/.config/stellar/<username>/backup/latest.toml` before creating the symlink.

This ensures your carefully crafted config is never lost! You can apply it anytime with:
```bash
stellar apply <username>/backup
```

You can also rename the backup folder to give it a proper theme name:
```bash
mv ~/.config/stellar/<username>/backup ~/.config/stellar/<username>/my-custom-theme
stellar apply <username>/my-custom-theme
```

### Switching between local configs

You can just put your own configs under `~/.config/stellar/local/<your-theme>/latest.toml`,
and then switch to them using `stellar apply local/<your-theme>`.

> [!INFO]
> The `/local` is not needed, you can actually use whatever you would like, i.e. `/<your-username>`,
> including existing usernames, just create an extra folder for your theme

### Customizing themes

You can similarily copy one existing downloaded theme to the `stellar/local` folder, edit it,
and then switch to it using `stellar apply`.

> [!INFO]
> @ here again, you don't need `/local`, so you can theoretically just copy for example
> `a3chron/ctp-red/latest.toml` to `a3chron/my-own-theme/latest.toml`

Because stellar is using a symlink to the currently selected config file, you get hot-reload as well.

## Contributing

All contributions are welcome :)  
The easiest way to contribute to stellar is to [upload you own starship config](https://stellar-hub.vercel.app/upload) for other to use.

Please use [conventional commits](https://www.conventionalcommits.org/) for PRs,
and check for lint errors with `golangci-lint run` (included in the flake).

### vhs

To record a vhs video just run:
```bash
vhs demo.tape
```

For nix users:
```bash
nix-shell -p vhs
```

## TODOs

- **`stellar publish` command**: Upload local themes directly to stellar-hub
  - Challenge: Need to implement CLI authentication (OAuth flow with browser redirect or API keys)
  - Would read from `~/.config/stellar/<author>/<theme>/latest.toml`
  - Interactive prompts for metadata (name, description, screenshot, etc.)
  - Skip complex fields initially (e.g., color scheme selection - add later)
- **`stellar update <theme>` command**: Update an existing theme on stellar-hub with a new version
  - Requires authentication (same challenge as publish)
  - Upload new version of already published theme
  - Interactive prompts for version notes, dependencies, etc.
- Add progress bars for downloads
- Improve error messages
- Add tests

<br />

<p align="center"><a href="https://github.com/a3chron/stellar/blob/main/LICENSE"><img alt="GitHub License" src="https://img.shields.io/github/license/a3chron/stellar?style=for-the-badge&labelColor=363a4f&color=b7bdf8">
</a></p>

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

### Switching between local configs

You can just put your own configs under `~/.config/stellar/local/<your-theme>/latest.toml`, 
and then switch to them using `stellar apply local/<your-theme>`.

### Customizing themes

You can similarily copy one existing downloaded theme to the `stellar/local` folder, edit it, 
and then switch to it using `stellar apply`.

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

- Add progress bars for downloads
- Improve error messages
- Add tests
- Set up GitHub Actions for releases
- Create Homebrew formula (macOS)?
- make sure this works for just local themes as well, i.e. a user can save his own themes in .config/stellar/username/my-config-01
- add docs for editing themes, i.e. copy theme directory into own local user directory, switch to local version, edit it
- add hot reload for developing themes / a reload comand (altough, should work with symlink? need to test)
- Add alias command, to use for example 'stellar apply blue' instead of 'stellar apply a3chron/ctp-blue'

<br />

<p align="center"><a href="https://github.com/a3chron/stellar/blob/main/LICENSE"><img alt="GitHub License" src="https://img.shields.io/github/license/a3chron/stellar?style=for-the-badge&labelColor=363a4f&color=b7bdf8">
</a></p>
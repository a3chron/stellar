# stellar-cli
Easily get and switch between starship configs

![Starship theme switcher demo](assets/demo.gif)

## Installation

Just run the [install script](https://raw.githubusercontent.com/a3chron/stellar/main/install.sh) 
which will download the binary and move it to `/usr/local/bin/`
```bash
curl -fsSL https://raw.githubusercontent.com/a3chron/stellar/main/install.sh | bash
```

## Why use & features

Getting good starship configs so far was mostly random, in some potentially weird guys github dotfiles, searching for something entirely else...  
With stellar, finding beautiful configs and getting them on your machine is as simple as \<insert something simple>.

You can also use themes from the community as boilerplates, customize them to be your own, and [switch between you own themes](#switching-between-local-configs) with stellar.

No more manually copying starship configs to switch the theme everytime you change you background image (In case you did that... (I did))

You can also use stellar in scripts for bigger changes, for example switching you entire systems accent color, or light & dark mode, who knows.

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


## TODOs

- Add progress bars for downloads
- Improve error messages
- Add tests
- Set up GitHub Actions for releases
- Create Homebrew formula (macOS)?
- make sure this works for just local themes as well, i.e. a user can save his own themes in .config/stellar/username/my-config-01
- add docs for editing themes, i.e. copy theme directory into own local user directory, switch to local version, edit it
- add hot reload for developing themes / a reload comand (altough, should work with symlink? need to test)
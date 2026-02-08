# stellar-cli
CLI for stellar

![Starship theme switcher demo](assets/demo.gif)

Getting good starship configs so far was mostly random, in some potentially weird guys github dotfiles, searching for something entirely else...  
With stellar, finding beautiful configs and getting them on your machine is as simple as \<insert something simple>.

You can also use themes from the community as boilerplates, customize them to be your own, and [switch between you own themes](#switching-between-local-configs) with stellar.

No more manually copying starship configs to switch the theme everytime you change you background image (In case you did that... (I did))

You can also use stellar in scripts for bigger changes, for example switching you entire systems accent color, or light & dark mode, who knows.

## Installation

Just run the [install script](https://raw.githubusercontent.com/a3chron/stellar/main/install.sh) 
which will download the binary and move it to `/usr/local/bin/`
```bash
curl -fsSL https://raw.githubusercontent.com/a3chron/stellar/main/install.sh | bash
```

## Basic Usage

```bash
# Apply a theme / config
stellar apply a3chron/ctp-blue
stellar apply a3chron/ctp-blue@1.2

# Preview before applying
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

# Rollback to previous
stellar rollback

# Update CLI
stellar update
```

## Local configs

### Switching between local configs

### Customizing themes

## Contributing

All contributions are welcome :)  
The easiest way to contribute to stellar is to [upload you own starship config](https://stellar-hub.vercel.app/upload) for other to use.

Please use [conventional commits](https://www.conventionalcommits.org/) for PRs, and don't forget to run `pnpm format && pnpm lint` from time to time ^^

## TODOs

- Add progress bars for downloads
- Improve error messages
- Add tests
- Set up GitHub Actions for releases
- Create Homebrew formula (macOS)?
- make sure this works for just local themes as well, i.e. a user can save his own themes in .config/stellar/username/my-config-01
- add docs for editing themes, i.e. copy theme directory into own local user directory, switch to local version, edit it
- add hot reload for developing themes / a reload comand (altough, should work with symlink? need to test)
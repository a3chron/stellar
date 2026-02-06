# stellar-cli
CLI for stellar



## Usage Examples

```bash
# Apply a theme
stellar apply alice/rainbow
stellar apply alice/rainbow@1.2

# Preview before applying
stellar preview bob/ocean

# List cached themes
stellar list

# Show current theme
stellar current

# Get theme info
stellar info alice/rainbow

# Clean cache (keep current)
stellar clean

# Remove specific theme
stellar remove alice/rainbow

# Rollback to previous
stellar rollback

# Update CLI
stellar update
```

## TODOs

1. Add progress bars for downloads
3. Improve error messages
4. Add tests
5. Set up GitHub Actions for releases
6. Create Homebrew formula (macOS)?
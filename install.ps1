#Requires -Version 5.1
# Installer for stellar on Windows.
# Usage: irm https://raw.githubusercontent.com/a3chron/stellar/main/install.ps1 | iex

$ErrorActionPreference = "Stop"

# Configurable install directory (override with $env:STELLAR_INSTALL_DIR)
$BinDir = if ($env:STELLAR_INSTALL_DIR) { $env:STELLAR_INSTALL_DIR } else { Join-Path $env:LOCALAPPDATA "stellar\bin" }

# Detect architecture
switch ($env:PROCESSOR_ARCHITECTURE) {
    "AMD64" { $Arch = "amd64" }
    "ARM64" { $Arch = "arm64" }
    default {
        Write-Error "Unsupported architecture: $($env:PROCESSOR_ARCHITECTURE)"
        exit 1
    }
}

$Binary = "stellar-windows-$Arch.exe"
$Url = "https://github.com/a3chron/stellar/releases/latest/download/$Binary"
$Target = Join-Path $BinDir "stellar.exe"

Write-Host "Installing stellar for windows-$Arch"
Write-Host "Target directory: $BinDir"

New-Item -ItemType Directory -Force -Path $BinDir | Out-Null

Invoke-WebRequest -Uri $Url -OutFile $Target -UseBasicParsing

Write-Host "stellar installed successfully!"

# Add install dir to the user PATH if it isn't already there
$UserPath = [Environment]::GetEnvironmentVariable("Path", "User")
if ($UserPath -notlike "*$BinDir*") {
    Write-Host ""
    Write-Host "Adding $BinDir to your user PATH"
    $NewPath = if ([string]::IsNullOrEmpty($UserPath)) { $BinDir } else { "$UserPath;$BinDir" }
    [Environment]::SetEnvironmentVariable("Path", $NewPath, "User")
    # Make it available in the current session too
    $env:Path = "$env:Path;$BinDir"
    Write-Host "Restart your terminal for the PATH change to take effect everywhere."
}

Write-Host ""
Write-Host "Run:"
Write-Host "  stellar --help"

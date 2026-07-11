#Requires -Version 5.1
# Installer for stellar on Windows.
# Usage: irm https://raw.githubusercontent.com/a3chron/stellar/main/install.ps1 | iex

$ErrorActionPreference = "Stop"

# Configurable install directory (override with $env:STELLAR_INSTALL_DIR)
$BinDir = if ($env:STELLAR_INSTALL_DIR) { $env:STELLAR_INSTALL_DIR } else { Join-Path $env:LOCALAPPDATA "stellar\bin" }

# Detect architecture. On a 32-bit process running under WoW64 (32-bit
# PowerShell on a 64-bit Windows host), PROCESSOR_ARCHITECTURE reports "x86"
# even though the OS is 64-bit; PROCESSOR_ARCHITEW6432 holds the real
# architecture in that case, so it must be checked first.
$RawArch = if ($env:PROCESSOR_ARCHITEW6432) { $env:PROCESSOR_ARCHITEW6432 } else { $env:PROCESSOR_ARCHITECTURE }
switch ($RawArch) {
    "AMD64" { $Arch = "amd64" }
    "ARM64" { $Arch = "arm64" }
    default {
        throw "Unsupported architecture: $RawArch"
    }
}

$Binary = "stellar-windows-$Arch.exe"
$Url = "https://github.com/a3chron/stellar/releases/latest/download/$Binary"
$ChecksumsUrl = "https://github.com/a3chron/stellar/releases/latest/download/checksums.txt"
$Target = Join-Path $BinDir "stellar.exe"
$DownloadPath = "$Target.download"

Write-Host "Installing stellar for windows-$Arch"
Write-Host "Target directory: $BinDir"

New-Item -ItemType Directory -Force -Path $BinDir | Out-Null

# Download to a temp path first (mirrors what "stellar update" already does):
# verify the checksum before ever touching the final "stellar.exe", so a
# corrupted or interrupted download never leaves a broken binary in place.
try {
    Write-Host "Downloading $Binary..."
    Invoke-WebRequest -Uri $Url -OutFile $DownloadPath -UseBasicParsing

    Write-Host "Fetching checksums..."
    $Checksums = (Invoke-WebRequest -Uri $ChecksumsUrl -UseBasicParsing).Content

    $ExpectedHash = $null
    foreach ($line in ($Checksums -split "`r?`n")) {
        if ([string]::IsNullOrWhiteSpace($line)) { continue }
        $fields = $line -split '\s+' | Where-Object { $_ }
        if ($fields.Count -lt 2) { continue }
        if ($fields[-1] -eq $Binary) {
            $ExpectedHash = $fields[0]
            break
        }
    }

    if (-not $ExpectedHash) {
        throw "checksum not found for binary: $Binary"
    }

    Write-Host "Verifying checksum..."
    $ActualHash = (Get-FileHash -Path $DownloadPath -Algorithm SHA256).Hash

    if ($ActualHash.ToLower() -ne $ExpectedHash.ToLower()) {
        throw "checksum verification failed for $Binary`n  expected: $ExpectedHash`n  got:      $ActualHash`n`nThe downloaded file may be corrupted or tampered with. Please try again"
    }

    Write-Host "Checksum verified successfully"

    Move-Item -Path $DownloadPath -Destination $Target -Force
}
catch {
    if (Test-Path $DownloadPath) {
        Remove-Item -Path $DownloadPath -Force -ErrorAction SilentlyContinue
    }
    throw
}

Write-Host "stellar installed successfully!"

# Add install dir to the user PATH if it isn't already there.
# Compare entry-by-entry (ignoring a trailing backslash) instead of a substring
# match, so an unrelated path that merely contains $BinDir as a prefix doesn't
# make us skip the update.
#
# Read/write the registry value directly via Microsoft.Win32.Registry (rather
# than [Environment]::GetEnvironmentVariable/SetEnvironmentVariable) so an
# existing REG_EXPAND_SZ "Path" entry (one containing unexpanded references
# like %SystemRoot%) is preserved as REG_EXPAND_SZ instead of being flattened
# to a REG_SZ, which would break those references for every other program
# reading the raw registry value.
$EnvKey = [Microsoft.Win32.Registry]::CurrentUser.OpenSubKey('Environment', $true)
$RawPath = $EnvKey.GetValue('Path', '', [Microsoft.Win32.RegistryValueOptions]::DoNotExpandEnvironmentNames)

$Normalized = $BinDir.TrimEnd('\')
$PathEntries = ($RawPath -split ';') | ForEach-Object { $_.TrimEnd('\') } | Where-Object { $_ }
if ($PathEntries -notcontains $Normalized) {
    Write-Host ""
    Write-Host "Adding $BinDir to your user PATH"

    # PowerShell 5.1 has no ternary operator, so use if/else explicitly.
    if ($RawPath) {
        $NewPath = "$RawPath;$BinDir"
    }
    else {
        $NewPath = $BinDir
    }

    if ($RawPath) {
        $Kind = $EnvKey.GetValueKind('Path')
    }
    else {
        $Kind = [Microsoft.Win32.RegistryValueKind]::ExpandString
    }

    $EnvKey.SetValue('Path', $NewPath, $Kind)

    # Make it available in the current session too
    $env:Path = "$env:Path;$BinDir"
    Write-Host "Restart your terminal for the PATH change to take effect everywhere."
}
$EnvKey.Close()

Write-Host ""
Write-Host "Run:"
Write-Host "  stellar --help"

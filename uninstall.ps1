#Requires -Version 5.1
# Uninstaller for stellar on Windows.
# Usage: irm https://raw.githubusercontent.com/a3chron/stellar/main/uninstall.ps1 | iex
#
# Prefers `stellar uninstall`, which tells the hub this install is gone and
# removes the stellar directory and binary; then removes the install directory
# from the user PATH (the inverse of install.ps1). Falls back to removing the
# files directly for builds that predate the command.

$ErrorActionPreference = "Stop"

# Same default as install.ps1 (override with $env:STELLAR_INSTALL_DIR)
$BinDir = if ($env:STELLAR_INSTALL_DIR) { $env:STELLAR_INSTALL_DIR } else { Join-Path $env:LOCALAPPDATA "stellar\bin" }
$Target = Join-Path $BinDir "stellar.exe"
$StellarHome = if ($env:STELLAR_HOME) { $env:STELLAR_HOME } else { Join-Path $env:USERPROFILE ".config\stellar" }

$StellarBin = $null
if (Test-Path $Target) {
    $StellarBin = $Target
}
else {
    $OnPath = Get-Command stellar -ErrorAction SilentlyContinue
    if ($OnPath) { $StellarBin = $OnPath.Source }
}

$HasUninstall = $false
if ($StellarBin) {
    & $StellarBin uninstall --help *> $null
    $HasUninstall = ($LASTEXITCODE -eq 0)
}

if ($HasUninstall) {
    # The binary renames itself aside and schedules its own deletion.
    & $StellarBin uninstall --yes
}
else {
    Write-Host "Removing stellar manually (this build has no uninstall command)"
    if (Test-Path $StellarHome) {
        Remove-Item -Path $StellarHome -Recurse -Force
        Write-Host "Removed $StellarHome"
    }
    if ($StellarBin -and (Test-Path $StellarBin)) {
        Remove-Item -Path $StellarBin -Force
        Write-Host "Removed $StellarBin"
    }
}

# Remove the install dir from the user PATH. Read/write the raw registry value
# via Microsoft.Win32.Registry, exactly as install.ps1 does, so a REG_EXPAND_SZ
# "Path" stays REG_EXPAND_SZ and unexpanded %VAR% references survive.
$EnvKey = [Microsoft.Win32.Registry]::CurrentUser.OpenSubKey('Environment', $true)
$RawPath = $EnvKey.GetValue('Path', '', [Microsoft.Win32.RegistryValueOptions]::DoNotExpandEnvironmentNames)

if ($RawPath) {
    $Normalized = $BinDir.TrimEnd('\')
    # Compare each entry EXPANDED (so %LOCALAPPDATA%\stellar\bin matches) but
    # keep the raw entries we write back.
    $Kept = @()
    $Removed = $false
    foreach ($entry in ($RawPath -split ';')) {
        if (-not $entry) { continue }
        $expanded = [Environment]::ExpandEnvironmentVariables($entry).TrimEnd('\')
        if ($expanded -eq $Normalized) {
            $Removed = $true
            continue
        }
        $Kept += $entry
    }
    if ($Removed) {
        $Kind = $EnvKey.GetValueKind('Path')
        $EnvKey.SetValue('Path', ($Kept -join ';'), $Kind)
        Write-Host "Removed $BinDir from your user PATH"
    }
}
$EnvKey.Close()

# Remove the now-empty install directory (the .old binary may linger for a
# moment while stellar's own deleter waits for the process to exit).
if ((Test-Path $BinDir) -and -not (Get-ChildItem -Path $BinDir -Force | Where-Object { $_.Name -ne 'stellar.exe.old' })) {
    Start-Sleep -Seconds 3
    Remove-Item -Path $BinDir -Recurse -Force -ErrorAction SilentlyContinue
}

Write-Host ""
Write-Host "stellar has been uninstalled. Restart your terminal for the PATH change to take effect."

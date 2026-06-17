# cipherbond bridge installer (Windows).
#
#   irm https://raw.githubusercontent.com/salehkreiner/bridge-claude-code/main/install.ps1 | iex
#
# Downloads the prebuilt scrub-claude.exe and scrub-setup.exe from GitHub
# Releases, verifies checksums when available, puts them on your user PATH, and
# runs scrub-setup once. It does nothing else.
#
# Environment overrides:
#   CipherBond_INSTALL_DIR   where to install   (default: %LOCALAPPDATA%\CipherBond\bin)
#   CipherBond_VERSION       release tag to get (default: latest)
#   CIPHERBOND_HUB_URL       Hub URL passed through to scrub-setup

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

$repo = 'salehkreiner/bridge-claude-code'

$installDir = if ($env:CipherBond_INSTALL_DIR) {
    $env:CipherBond_INSTALL_DIR
} else {
    Join-Path $env:LOCALAPPDATA 'CipherBond\bin'
}
$version = if ($env:CipherBond_VERSION) { $env:CipherBond_VERSION } else { 'latest' }

# Only windows/amd64 is published in the release matrix.
$arch = 'amd64'
$suffix = "windows_$arch"

$base = if ($version -eq 'latest') {
    "https://github.com/$repo/releases/latest/download"
} else {
    "https://github.com/$repo/releases/download/$version"
}

Write-Host "Installing the cipherbond bridge ($suffix) into $installDir"
New-Item -ItemType Directory -Force -Path $installDir | Out-Null

# --- optional checksum verification -----------------------------------------
$sums = $null
try {
    $sums = (Invoke-WebRequest -Uri "$base/SHA256SUMS" -UseBasicParsing).Content
} catch {
    Write-Host "  (no SHA256SUMS published; skipping checksum verification)"
}

function Test-Checksum($file, $asset) {
    if (-not $sums) { return }
    $line = ($sums -split "`n") | Where-Object { $_ -match [regex]::Escape($asset) } | Select-Object -First 1
    if (-not $line) {
        Write-Host "  (no checksum entry for $asset; skipping)"
        return
    }
    $expected = ($line -split '\s+')[0]
    $actual = (Get-FileHash -Algorithm SHA256 -Path $file).Hash.ToLower()
    if ($expected.ToLower() -ne $actual) {
        throw "checksum mismatch for $asset (expected $expected, got $actual)"
    }
}

# --- download binaries -------------------------------------------------------
foreach ($bin in 'scrub-claude', 'scrub-setup') {
    $asset = "${bin}_${suffix}.exe"
    $out = Join-Path $installDir "$bin.exe"
    Write-Host "  downloading $asset"
    Invoke-WebRequest -Uri "$base/$asset" -OutFile $out -UseBasicParsing
    Test-Checksum $out $asset
}
Write-Host "Installed scrub-claude.exe and scrub-setup.exe."

# --- ensure PATH (user scope) ------------------------------------------------
$userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
if (-not $userPath) { $userPath = '' }
if (($userPath -split ';') -notcontains $installDir) {
    $newPath = if ($userPath) { "$userPath;$installDir" } else { $installDir }
    [Environment]::SetEnvironmentVariable('Path', $newPath, 'User')
    Write-Host "Added $installDir to your user PATH."
}
# Make it available for the rest of this session too.
if (($env:Path -split ';') -notcontains $installDir) {
    $env:Path = "$env:Path;$installDir"
}

# --- run setup ---------------------------------------------------------------
Write-Host ""
Write-Host "Running scrub-setup..."
$setup = Join-Path $installDir 'scrub-setup.exe'
$setupArgs = @('--yes')
if ($env:CIPHERBOND_HUB_URL) { $setupArgs += @('--hub-url', $env:CIPHERBOND_HUB_URL) }
& $setup @setupArgs

Write-Host ""
Write-Host "Done. Open a new terminal so PATH and the 'claude' function take effect."

[CmdletBinding()]
param(
    [ValidateSet("install", "uninstall")]
    [string]$Action = $(if ($env:VERDANA_ACTION) { $env:VERDANA_ACTION } else { "install" }),
    [string]$Version = $(if ($env:VERDANA_VERSION) { $env:VERDANA_VERSION } else { "latest" }),
    [string]$InstallDir = $(if ($env:VERDANA_INSTALL_DIR) { $env:VERDANA_INSTALL_DIR } else { Join-Path $env:LOCALAPPDATA "Programs\Verdana" }),
    [switch]$Purge
)

$ErrorActionPreference = "Stop"
$binary = Join-Path $InstallDir "verdana.exe"
$dataDir = Join-Path $env:APPDATA "Verdana"

function Set-UserPathEntry([string]$Directory, [bool]$Present) {
    $current = [Environment]::GetEnvironmentVariable("Path", "User")
    $parts = @($current -split ";" | Where-Object { $_ -and $_ -ne $Directory })
    if ($Present) { $parts += $Directory }
    [Environment]::SetEnvironmentVariable("Path", ($parts -join ";"), "User")
    $env:Path = (($env:Path -split ";" | Where-Object { $_ -and $_ -ne $Directory }) + $(if ($Present) { $Directory } else { @() })) -join ";"
}

function Stop-InstalledVerdana {
    if (-not (Test-Path $binary)) { return }
    Get-Process verdana -ErrorAction SilentlyContinue | ForEach-Object {
        try {
            if ($_.Path -eq $binary) {
                Stop-Process -Id $_.Id -Force
                $_.WaitForExit()
            }
        } catch {
            # A protected or already-exited process does not prevent the
            # subsequent file operation from reporting a useful error.
        }
    }
}

if ($Action -eq "uninstall") {
    Stop-InstalledVerdana
    if (Test-Path $binary) {
        Remove-Item -LiteralPath $binary -Force
        Write-Host "Removed $binary"
    } else {
        Write-Host "Verdana is not installed at $binary"
    }
    if (Test-Path $InstallDir) {
        $remaining = @(Get-ChildItem -LiteralPath $InstallDir -Force)
        if ($remaining.Count -eq 0) { Remove-Item -LiteralPath $InstallDir -Force }
    }
    Set-UserPathEntry $InstallDir $false
    Remove-ItemProperty -Path "HKCU:\Software\Microsoft\Windows\CurrentVersion\Run" -Name "Verdana" -ErrorAction SilentlyContinue
    $programs = Join-Path $env:APPDATA "Microsoft\Windows\Start Menu\Programs"
    Get-ChildItem -Path (Join-Path $programs "verdana-*.lnk") -ErrorAction SilentlyContinue | Remove-Item -Force
    $appLinks = Join-Path $programs "Verdana Apps"
    if (Test-Path $appLinks) { Remove-Item -LiteralPath $appLinks -Recurse -Force }
    if ($Purge) {
        if (Test-Path $dataDir) { Remove-Item -LiteralPath $dataDir -Recurse -Force }
        Write-Host "Removed user data at $dataDir"
    } else {
        Write-Host "User data was kept at $dataDir (use -Purge to remove it)."
    }
    exit 0
}

$machine = if ($env:PROCESSOR_ARCHITEW6432) { $env:PROCESSOR_ARCHITEW6432 } else { $env:PROCESSOR_ARCHITECTURE }
$arch = switch ($machine.ToUpperInvariant()) {
    "AMD64" { "amd64" }
    "ARM64" { "arm64" }
    default { throw "Unsupported Windows architecture: $machine" }
}

$asset = "verdana-windows-$arch.zip"
if ($Version -eq "latest") {
    $baseUrl = "https://github.com/hzrd149/verdana/releases/latest/download"
} else {
    if (-not $Version.StartsWith("v")) { $Version = "v$Version" }
    $baseUrl = "https://github.com/hzrd149/verdana/releases/download/$Version"
}

$tempDir = Join-Path ([IO.Path]::GetTempPath()) ("verdana-" + [guid]::NewGuid())
New-Item -ItemType Directory -Path $tempDir | Out-Null
try {
    $archive = Join-Path $tempDir $asset
    $checksums = Join-Path $tempDir "SHA256SUMS"
    Write-Host "Downloading $asset..."
    Invoke-WebRequest "$baseUrl/$asset" -OutFile $archive
    Invoke-WebRequest "$baseUrl/SHA256SUMS" -OutFile $checksums

    $line = Get-Content $checksums | Where-Object { $_ -match "^[0-9a-fA-F]{64}\s+\*?$([regex]::Escape($asset))$" } | Select-Object -First 1
    if (-not $line) { throw "No checksum found for $asset." }
    $expected = ($line -split "\s+")[0].ToLowerInvariant()
    $actual = (Get-FileHash -Algorithm SHA256 $archive).Hash.ToLowerInvariant()
    if ($actual -ne $expected) { throw "Checksum verification failed." }

    $expanded = Join-Path $tempDir "expanded"
    Expand-Archive -LiteralPath $archive -DestinationPath $expanded
    New-Item -ItemType Directory -Path $InstallDir -Force | Out-Null
    Stop-InstalledVerdana
    Copy-Item -LiteralPath (Join-Path $expanded "verdana.exe") -Destination $binary -Force
    Set-UserPathEntry $InstallDir $true
    Write-Host "Installed Verdana at $binary"
    Write-Host "Open a new terminal to run 'verdana'."
} finally {
    if (Test-Path $tempDir) { Remove-Item -LiteralPath $tempDir -Recurse -Force }
}

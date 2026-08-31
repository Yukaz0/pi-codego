# Pi-Go installer (Windows) — irm https://raw.githubusercontent.com/Yukaz0/pi-codego/main/scripts/install.ps1 | iex
$ErrorActionPreference = "Stop"

# --- CONFIG: ganti ke repo GitHub kamu setelah push ---
$Repo = if ($env:PI_GO_REPO) { $env:PI_GO_REPO } else { "Yukaz0/pi-codego" }
$BinName = "pi-go"

$arch = if ([Environment]::Is64BitOperatingSystem) {
    if ($env:PROCESSOR_ARCHITECTURE -eq "ARM64") { "arm64" } else { "amd64" }
} { throw "32-bit Windows is not supported" }

$asset = "${BinName}-windows-${arch}.exe"
Write-Host "-> fetching latest release of $Repo..."

$rel = Invoke-RestMethod -Uri "https://api.github.com/repos/$Repo/releases/latest"
$dl = $rel.assets | Where-Object { $_.name -eq $asset } | Select-Object -First 1
if (-not $dl) { throw "asset $asset not found in latest release" }

$installDir = Join-Path $env:LOCALAPPDATA "Programs\pi-go"
New-Item -ItemType Directory -Force -Path $installDir | Out-Null

$dest = Join-Path $installDir "$BinName.exe"
Write-Host "-> downloading $asset"
Invoke-WebRequest -Uri $dl.browser_download_url -OutFile $dest

$userPath = [Environment]::GetEnvironmentVariable("Path", "User")
if ($userPath -notlike "*$installDir*") {
    [Environment]::SetEnvironmentVariable("Path", "$userPath;$installDir", "User")
    Write-Host "-> added $installDir to user PATH (restart your terminal)"
}

Write-Host ""
Write-Host "OK installed: $dest"
Write-Host "Run: $BinName --help"

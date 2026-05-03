param(
  [string]$Version = '',
  [string]$BinDir = '',
  [switch]$DryRun,
  [switch]$Help
)

$ErrorActionPreference = 'Stop'

$RepoOwner = if ($env:REPO_OWNER) { $env:REPO_OWNER } else { 'swobuforge' }
$RepoName = if ($env:REPO_NAME) { $env:REPO_NAME } else { 'swobu' }
$ProjectName = if ($env:PROJECT_NAME) { $env:PROJECT_NAME } else { 'swobu' }
$BinName = if ($env:BIN_NAME) { $env:BIN_NAME } else { 'swobu' }
$InstallDir = if ($env:INSTALL_DIR) { $env:INSTALL_DIR } elseif ($BinDir) { $BinDir } else { Join-Path $HOME '.local/bin' }
if (-not $Version -and $env:VERSION) { $Version = $env:VERSION }
if (-not $DryRun -and $env:DRY_RUN) { $DryRun = [System.Convert]::ToBoolean($env:DRY_RUN) }

function Show-Usage {
  @"
Install swobu from GitHub Releases.

Usage:
  install.ps1 [-Version vX.Y.Z] [-BinDir /path] [-DryRun]

Environment overrides:
  REPO_OWNER, REPO_NAME, PROJECT_NAME, BIN_NAME, INSTALL_DIR, VERSION, DRY_RUN
"@
}

if ($Help) {
  Show-Usage
  exit 0
}

if ([string]::IsNullOrWhiteSpace($Version)) {
  $latestUrl = "https://api.github.com/repos/$RepoOwner/$RepoName/releases/latest"
  $latest = Invoke-RestMethod -Uri $latestUrl
  if (-not $latest.tag_name) {
    throw "failed to resolve latest release tag from $latestUrl"
  }
  $Version = [string]$latest.tag_name
}

$os = 'windows'
$archRaw = [System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture.ToString().ToLowerInvariant()
switch ($archRaw) {
  'x64' { $arch = 'amd64' }
  'arm64' { $arch = 'arm64' }
  default { throw "unsupported architecture: $archRaw (supported: amd64, arm64)" }
}

$archive = "${ProjectName}_${Version}_${os}_${arch}.zip"
$baseUrl = "https://github.com/$RepoOwner/$RepoName/releases/download/$Version"
$archiveUrl = "$baseUrl/$archive"
$checksumsUrl = "$baseUrl/checksums.txt"

if ($DryRun) {
  Write-Output "tag=$Version"
  Write-Output "os=$os"
  Write-Output "arch=$arch"
  Write-Output "archive=$archive"
  Write-Output "archive_url=$archiveUrl"
  Write-Output "checksums_url=$checksumsUrl"
  Write-Output "install_dir=$InstallDir"
  exit 0
}

New-Item -ItemType Directory -Force -Path $InstallDir | Out-Null
$tmpRoot = Join-Path ([System.IO.Path]::GetTempPath()) ("swobu-install-" + [System.Guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Force -Path $tmpRoot | Out-Null

try {
  $archivePath = Join-Path $tmpRoot $archive
  $checksumsPath = Join-Path $tmpRoot 'checksums.txt'

  Write-Output "downloading: $archiveUrl"
  Invoke-WebRequest -Uri $archiveUrl -OutFile $archivePath
  Write-Output "downloading: $checksumsUrl"
  Invoke-WebRequest -Uri $checksumsUrl -OutFile $checksumsPath

  $expected = ''
  Get-Content -Path $checksumsPath | ForEach-Object {
    $line = $_.Trim()
    if ([string]::IsNullOrWhiteSpace($line)) { return }
    $parts = $line -split '\s+', 2
    if ($parts.Count -eq 2 -and $parts[1].Trim() -eq $archive) {
      $expected = $parts[0].Trim()
    }
  }
  if ([string]::IsNullOrWhiteSpace($expected)) {
    throw "archive $archive not found in checksums.txt"
  }

  $actual = (Get-FileHash -Algorithm SHA256 -Path $archivePath).Hash.ToLowerInvariant()
  if ($expected.ToLowerInvariant() -ne $actual) {
    throw "checksum mismatch for $archive"
  }

  $extractDir = Join-Path $tmpRoot 'extract'
  New-Item -ItemType Directory -Force -Path $extractDir | Out-Null
  Expand-Archive -Path $archivePath -DestinationPath $extractDir -Force

  $sourceExe = Join-Path $extractDir ("$BinName.exe")
  if (-not (Test-Path -Path $sourceExe -PathType Leaf)) {
    throw "archive missing binary: $BinName.exe"
  }

  $installPath = Join-Path $InstallDir ("$BinName.exe")
  Copy-Item -Path $sourceExe -Destination $installPath -Force
  Write-Output "installed $BinName to $installPath"
}
finally {
  if (Test-Path -Path $tmpRoot) {
    Remove-Item -Path $tmpRoot -Recurse -Force -ErrorAction SilentlyContinue
  }
}

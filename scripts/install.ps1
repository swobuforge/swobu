param(
  [string]$Version = '',
  [string]$BinDir = '',
  [string]$Checksum = '',
  [switch]$DryRun,
  [switch]$Help
)

$ErrorActionPreference = 'Stop'

$RepoOwner = if ($env:REPO_OWNER) { $env:REPO_OWNER } else { 'swobuforge' }
$RepoName = if ($env:REPO_NAME) { $env:REPO_NAME } else { 'swobu' }
$ProjectName = if ($env:PROJECT_NAME) { $env:PROJECT_NAME } else { 'swobu' }
$BinName = if ($env:BIN_NAME) { $env:BIN_NAME } else { 'swobu' }
$InstallDir = if ($env:INSTALL_DIR) { $env:INSTALL_DIR } elseif ($BinDir) { $BinDir } else { Join-Path $HOME 'AppData/Local/Programs/swobu/bin' }
if (-not $Version -and $env:VERSION) { $Version = $env:VERSION }
if (-not $DryRun -and $env:DRY_RUN) { $DryRun = [System.Convert]::ToBoolean($env:DRY_RUN) }
if (-not $Checksum -and $env:EXPECTED_SHA256) { $Checksum = $env:EXPECTED_SHA256 }

function Show-Usage {
  @"
Install swobu from GitHub Releases.

Usage:
  install.ps1 [-Version vX.Y.Z] [-BinDir /path] [-Checksum <sha256>] [-DryRun]

Environment overrides:
  REPO_OWNER, REPO_NAME, PROJECT_NAME, BIN_NAME, INSTALL_DIR, VERSION, DRY_RUN, EXPECTED_SHA256
"@
}

function Normalize-Sha256 {
  param([Parameter(Mandatory = $true)][string]$Value)
  $trimmed = $Value.Trim().ToLowerInvariant()
  if ($trimmed -notmatch '^[0-9a-f]{64}$') {
    throw "invalid sha256 value: $Value"
  }
  return $trimmed
}

function Get-ExpectedChecksumFromFile {
  param(
    [Parameter(Mandatory = $true)][string]$ChecksumsPath,
    [Parameter(Mandatory = $true)][string]$ArchiveName
  )
  foreach ($line in Get-Content -Path $ChecksumsPath) {
    $trimmed = $line.Trim()
    if ([string]::IsNullOrWhiteSpace($trimmed)) { continue }
    if ($trimmed -match '^([0-9A-Fa-f]{64})\s+\*?(.+)$') {
      $name = $Matches[2].Trim()
      if ($name -eq $ArchiveName) {
        return Normalize-Sha256 -Value $Matches[1]
      }
    }
  }
  throw "archive $ArchiveName not found in checksums.txt"
}

function Extract-ZipEntrySafely {
  param(
    [Parameter(Mandatory = $true)][string]$ArchivePath,
    [Parameter(Mandatory = $true)][string]$EntryName,
    [Parameter(Mandatory = $true)][string]$DestinationPath
  )
  Add-Type -AssemblyName System.IO.Compression
  Add-Type -AssemblyName System.IO.Compression.FileSystem
  $zip = [System.IO.Compression.ZipFile]::OpenRead($ArchivePath)
  try {
    foreach ($entry in $zip.Entries) {
      if ($entry.FullName -match '(^[\\/])|(\.\.)') {
        throw "refusing suspicious archive entry path: $($entry.FullName)"
      }
    }
    $target = $zip.Entries | Where-Object { $_.FullName -eq $EntryName } | Select-Object -First 1
    if (-not $target) {
      throw "archive missing binary entry: $EntryName"
    }
    if ($target.FullName -match '[\\/]') {
      throw "refusing nested archive entry for binary: $($target.FullName)"
    }
    $outDir = Split-Path -Parent $DestinationPath
    if (-not (Test-Path -Path $outDir -PathType Container)) {
      New-Item -ItemType Directory -Force -Path $outDir | Out-Null
    }
    $inStream = $target.Open()
    try {
      $outStream = [System.IO.File]::Create($DestinationPath)
      try {
        $inStream.CopyTo($outStream)
      }
      finally {
        $outStream.Dispose()
      }
    }
    finally {
      $inStream.Dispose()
    }
  }
  finally {
    $zip.Dispose()
  }
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
  if ($Checksum) {
    Write-Output "expected_sha256=$(Normalize-Sha256 -Value $Checksum)"
  }
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

  Write-Output 'Verifying artifact checksum'
  $expected = Get-ExpectedChecksumFromFile -ChecksumsPath $checksumsPath -ArchiveName $archive
  $actual = Normalize-Sha256 -Value (Get-FileHash -Algorithm SHA256 -Path $archivePath).Hash
  if ($expected -ne $actual) {
    throw "error: checksum mismatch for $archive"
  }
  if ($Checksum) {
    $pinned = Normalize-Sha256 -Value $Checksum
    if ($pinned -ne $actual) {
      throw "pinned checksum mismatch for $archive"
    }
  }
  else { Write-Output 'pinned checksum not provided; integrity checked via release checksums.' }

  $extractDir = Join-Path $tmpRoot 'extract'
  $sourceExe = Join-Path $extractDir ("$BinName.exe")
  Extract-ZipEntrySafely -ArchivePath $archivePath -EntryName "$BinName.exe" -DestinationPath $sourceExe

  $installPath = Join-Path $InstallDir ("$BinName.exe")
  $tmpInstallPath = Join-Path $InstallDir (".$BinName.exe.tmp")
  Write-Output "Installing to $installPath"
  Copy-Item -Path $sourceExe -Destination $tmpInstallPath -Force
  Move-Item -Path $tmpInstallPath -Destination $installPath -Force
  Write-Output "$BinName installed successfully"
  Write-Output ''
  Write-Output 'Run:'
  Write-Output "  $installPath --version"

  $pathEntries = ($env:PATH -split ';') | ForEach-Object { $_.Trim() } | Where-Object { $_ -ne '' }
  if ($pathEntries -notcontains $InstallDir) {
    Write-Output ''
    Write-Output "Note: $InstallDir is not on your PATH."
    Write-Output "Add it to your PowerShell profile before running $BinName from a new terminal."
  }
}
finally {
  if (Test-Path -Path $tmpRoot) {
    Remove-Item -Path $tmpRoot -Recurse -Force -ErrorAction SilentlyContinue
  }
}

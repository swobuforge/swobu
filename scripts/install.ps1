param(
  [string]$Version = '',
  [string]$BinDir = '',
  [string]$Checksum = '',
  [switch]$DryRun,
  [switch]$NoStart,
  [switch]$Verbose,
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
if (-not $Verbose -and $env:VERBOSE) { $Verbose = [System.Convert]::ToBoolean($env:VERBOSE) }
$StartSwobu = -not $NoStart
if (-not $NoStart -and $env:START_SWOBU) { $StartSwobu = [System.Convert]::ToBoolean($env:START_SWOBU) }

function Say { param([string]$Message) Write-Host $Message }
function Step { param([string]$Message) Write-Host "→ $Message" }
function Ok { param([string]$Message) Write-Host "✓ $Message" }
function Warn { param([string]$Message) Write-Warning $Message }
function DebugLog {
  param([string]$Message)
  if ($Verbose) { Write-Host "debug: $Message" }
}
function Die {
  param([string]$Message)
  throw "error: $Message"
}

function Show-Usage {
  @"
Install swobu from GitHub Releases.

Usage:
  install.ps1 [-Version vX.Y.Z] [-BinDir /path] [-Checksum <sha256>] [-DryRun] [-NoStart] [-Verbose]

Environment overrides:
  REPO_OWNER, REPO_NAME, PROJECT_NAME, BIN_NAME, INSTALL_DIR, VERSION, DRY_RUN, EXPECTED_SHA256, VERBOSE, START_SWOBU
"@
}

function Normalize-Sha256 {
  param([Parameter(Mandatory = $true)][string]$Value)
  $trimmed = $Value.Trim().ToLowerInvariant()
  if ($trimmed -notmatch '^[0-9a-f]{64}$') {
    Die "invalid sha256 value: $Value"
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
  Step "Resolving latest release..."
  $latest = Invoke-RestMethod -Uri $latestUrl
  if (-not $latest.tag_name) {
    Die "failed to resolve latest release tag from $latestUrl"
  }
  $Version = [string]$latest.tag_name
}

$os = 'windows'
$archRaw = [System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture.ToString().ToLowerInvariant()
switch ($archRaw) {
  'x64' { $arch = 'amd64' }
  'arm64' { $arch = 'arm64' }
  default { Die "unsupported architecture: $archRaw (supported: amd64, arm64)" }
}

$archive = "${ProjectName}_${Version}_${os}_${arch}.zip"
$baseUrl = "https://github.com/$RepoOwner/$RepoName/releases/download/$Version"
$archiveUrl = "$baseUrl/$archive"
$checksumsUrl = "$baseUrl/checksums.txt"

if ($DryRun) {
  Say "Swobu installer dry-run"
  Write-Output "tag=$Version"
  Write-Output "os=$os"
  Write-Output "arch=$arch"
  Write-Output "archive=$archive"
  Write-Output "archive_url=$archiveUrl"
  Write-Output "checksums_url=$checksumsUrl"
  Write-Output "install_dir=$InstallDir"
  Write-Output "start_swobu=$($StartSwobu.ToString().ToLowerInvariant())"
  if ($Checksum) {
    Write-Output "expected_sha256=$(Normalize-Sha256 -Value $Checksum)"
  }
  exit 0
}

Say "Swobu installer"
Say ''
Step "Detecting platform... $os $arch"
Step "Preparing install directory... $InstallDir"
New-Item -ItemType Directory -Force -Path $InstallDir | Out-Null
$tmpRoot = Join-Path ([System.IO.Path]::GetTempPath()) ("swobu-install-" + [System.Guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Force -Path $tmpRoot | Out-Null

try {
  $archivePath = Join-Path $tmpRoot $archive
  $checksumsPath = Join-Path $tmpRoot 'checksums.txt'

  Step "Downloading $archive"
  Invoke-WebRequest -Uri $archiveUrl -OutFile $archivePath
  Step "Downloading checksums"
  Invoke-WebRequest -Uri $checksumsUrl -OutFile $checksumsPath

  Step 'Verifying checksum'
  $expected = Get-ExpectedChecksumFromFile -ChecksumsPath $checksumsPath -ArchiveName $archive
  $actual = Normalize-Sha256 -Value (Get-FileHash -Algorithm SHA256 -Path $archivePath).Hash
  if ($expected -ne $actual) {
    Die "checksum mismatch for $archive"
  }
  if ($Checksum) {
    $pinned = Normalize-Sha256 -Value $Checksum
    if ($pinned -ne $actual) {
      Die "pinned checksum mismatch for $archive"
    }
  }

  $extractDir = Join-Path $tmpRoot 'extract'
  $sourceExe = Join-Path $extractDir ("$BinName.exe")
  Extract-ZipEntrySafely -ArchivePath $archivePath -EntryName "$BinName.exe" -DestinationPath $sourceExe

  $installPath = Join-Path $InstallDir ("$BinName.exe")
  if (Test-Path -Path $installPath -PathType Container) {
    Die "$installPath exists and is a directory"
  }
  if (Test-Path -Path $installPath -PathType Leaf) {
    $existingVersion = (& $installPath --version 2>$null) -join "`n"
    if (-not [string]::IsNullOrWhiteSpace($existingVersion)) {
      Step "Found existing ${BinName}: $existingVersion"
    }
    else {
      Step "Found existing $BinName at $installPath"
    }
  }
  $tmpInstallPath = Join-Path $InstallDir (".$BinName.exe.tmp")
  Step "Installing to $installPath"
  Copy-Item -Path $sourceExe -Destination $tmpInstallPath -Force
  Move-Item -Path $tmpInstallPath -Destination $installPath -Force
  Step 'Checking installation'
  try {
    & $installPath --version *> $null
    $InstallationVerified = $true
    Ok "$BinName $Version installed"
  }
  catch {
    $InstallationVerified = $false
    Warn "$BinName was installed, but '$BinName --version' failed."
    Say "Try:"
    Say "  $installPath --version"
  }
  if ($InstallationVerified -and $StartSwobu) {
    Say ''
    Step 'Starting Swobu'
    try {
      $process = Start-Process -FilePath $installPath -Wait -PassThru
      if ($process.ExitCode -ne 0) {
        Warn "Swobu was installed, but startup exited with code $($process.ExitCode)."
        Say 'Try again:'
        Say "  $installPath"
      }
    }
    catch {
      Warn 'Swobu was installed, but startup failed.'
      Say 'Try again:'
      Say "  $installPath"
    }
  }
  elseif ($InstallationVerified) {
    Say ''
    Say 'Start Swobu:'
    Say "  $installPath"
  }

  $pathValue = if ($null -ne $env:PATH) { $env:PATH } else { '' }
  $pathEntries = ($pathValue -split ';') | ForEach-Object { $_.Trim() } | Where-Object { $_ -ne '' }
  if ($pathEntries -notcontains $InstallDir) {
    Say ''
    Warn "$InstallDir is not on your PATH."
    Say "Add it:"
    Say "  `$env:Path = ""$InstallDir;`$env:Path"""
    Say "Persist it:"
    Say "  [Environment]::SetEnvironmentVariable('Path', [Environment]::GetEnvironmentVariable('Path','User') + ';$InstallDir', 'User')"
  }
}
finally {
  if (Test-Path -Path $tmpRoot) {
    Remove-Item -Path $tmpRoot -Recurse -Force -ErrorAction SilentlyContinue
  }
}

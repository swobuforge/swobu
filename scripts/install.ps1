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
$InstallDir = if ($env:INSTALL_DIR) { $env:INSTALL_DIR } elseif ($BinDir) { $BinDir } elseif ($env:LOCALAPPDATA) { Join-Path $env:LOCALAPPDATA 'Programs/swobu/bin' } else { throw 'error: LOCALAPPDATA is required for the default installation path' }
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
  install.ps1 [-Version swobu-vX.Y.Z] [-BinDir /path] [-Checksum <sha256>] [-DryRun] [-NoStart] [-Verbose]

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

function Normalize-Version {
  param([Parameter(Mandatory = $true)][string]$Value)
  return ($Value.Trim() -replace '^swobu-', '') -replace '^v', ''
}

function Assert-ValidVersion {
  param([Parameter(Mandatory = $true)][string]$Value)
  if ($Value -notmatch '^(swobu-v|v?)[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z]+([.-][0-9A-Za-z]+)*)?$') {
    Die "invalid version: $Value (expected swobu-vX.Y.Z[-prerelease])"
  }
}

function Resolve-Architecture {
  param([Parameter(Mandatory = $true)][string]$Architecture)
  switch ($Architecture.ToLowerInvariant()) {
    'x64' { return 'amd64' }
    'arm64' { return 'arm64' }
    default { Die "unsupported architecture: $Architecture (supported: amd64, arm64)" }
  }
}

function Get-BinaryVersion {
  param([Parameter(Mandatory = $true)][string]$Path)
  try {
    $output = (& $Path --version 2>$null) -join "`n"
    if ($LASTEXITCODE -ne 0) { return $null }
    return $output.Trim()
  }
  catch {
    return $null
  }
}

function Assert-BinaryVersion {
  param(
    [Parameter(Mandatory = $true)][string]$Path,
    [Parameter(Mandatory = $true)][string]$ExpectedVersion,
    [Parameter(Mandatory = $true)][string]$Description
  )
  $actualVersion = Get-BinaryVersion -Path $Path
  if ([string]::IsNullOrWhiteSpace($actualVersion)) {
    Die "$Description failed '$BinName --version'"
  }
  if ((Normalize-Version -Value $actualVersion) -ne (Normalize-Version -Value $ExpectedVersion)) {
    Die "$Description reported $actualVersion, expected $ExpectedVersion"
  }
}

function Assert-StandaloneTarget {
  param([Parameter(Mandatory = $true)][string]$Path)
  $item = Get-Item -LiteralPath $Path -Force -ErrorAction SilentlyContinue
  if (-not $item) { return }
  if (($item.Attributes -band [System.IO.FileAttributes]::ReparsePoint) -ne 0) {
    Die "$Path is a reparse point; refusing to manage an indirect executable"
  }
  if ($item.PSIsContainer) {
    Die "$Path exists and is a directory"
  }
}

function Restore-PreviousExecutable {
  param(
    [Parameter(Mandatory = $true)][string]$PreviousPath,
    [Parameter(Mandatory = $true)][string]$InstallPath,
    [Parameter(Mandatory = $true)][string]$ActivationError
  )
  try {
    Move-Item -LiteralPath $PreviousPath -Destination $InstallPath -ErrorAction Stop
  }
  catch {
    throw "error: $ActivationError; restoration also failed: $($_.Exception.Message); previous executable remains at $PreviousPath"
  }
}

function Diagnose-Path {
  param(
    [Parameter(Mandatory = $true)][string]$InstallDir,
    [Parameter(Mandatory = $true)][string]$InstallPath
  )
  $pathValue = if ($null -ne $env:PATH) { $env:PATH } else { '' }
  $pathEntries = ($pathValue -split ';') | ForEach-Object { $_.Trim() } | Where-Object { $_ -ne '' }
  if (-not ($pathEntries | Where-Object { [string]::Equals($_.TrimEnd('\'), $InstallDir.TrimEnd('\'), [System.StringComparison]::OrdinalIgnoreCase) })) {
    Say ''
    Warn "$InstallDir is not on your PATH."
    Say "Add it:"
    Say "  `$env:Path = ""$InstallDir;`$env:Path"""
    Say "Persist it:"
    Say "  [Environment]::SetEnvironmentVariable('Path', [Environment]::GetEnvironmentVariable('Path','User') + ';$InstallDir', 'User')"
    return
  }
  $resolved = Get-Command "$BinName.exe" -CommandType Application -ErrorAction SilentlyContinue | Select-Object -First 1
  if ($resolved -and -not [string]::Equals($resolved.Source, $InstallPath, [System.StringComparison]::OrdinalIgnoreCase)) {
    Say ''
    Warn "$($resolved.Source) shadows $InstallPath on PATH."
    Say "Move $InstallDir before $(Split-Path -Parent $resolved.Source) in PATH."
  }
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

if (-not [string]::IsNullOrWhiteSpace($Version)) {
  Assert-ValidVersion -Value $Version
}

Say "Swobu installer"
Say ''
Step "Preparing install directory... $InstallDir"
$installPath = Join-Path $InstallDir ("$BinName.exe")
if (-not $DryRun) {
  New-Item -ItemType Directory -Force -Path $InstallDir | Out-Null
  Assert-StandaloneTarget -Path $installPath
  $previousFiles = @(Get-ChildItem -LiteralPath $InstallDir -Filter ".$BinName.previous.*.exe" -File -ErrorAction SilentlyContinue | Sort-Object LastWriteTimeUtc -Descending)
  if (-not (Test-Path -LiteralPath $installPath) -and $previousFiles.Count -gt 0) {
    $recoveryPath = $previousFiles[0].FullName
    Step "Recovering interrupted installation from $recoveryPath"
    try {
      Move-Item -LiteralPath $recoveryPath -Destination $installPath -ErrorAction Stop
    }
    catch {
      throw "error: failed to restore interrupted installation from $recoveryPath to ${installPath}: $($_.Exception.Message)"
    }
    Assert-StandaloneTarget -Path $installPath
  }
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
Assert-ValidVersion -Value $Version

$os = 'windows'
$archRaw = [System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture.ToString().ToLowerInvariant()
$arch = Resolve-Architecture -Architecture $archRaw

$productVersion = $Version -replace '^swobu-', ''
$archive = "${ProjectName}_${productVersion}_${os}_${arch}.zip"
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

Step "Detecting platform... $os $arch"
$freshInstall = -not (Test-Path -LiteralPath $installPath)
if (-not $freshInstall) {
  $existingVersion = Get-BinaryVersion -Path $installPath
  if (-not [string]::IsNullOrWhiteSpace($existingVersion)) {
    Step "Found existing ${BinName}: $existingVersion"
    if ((Normalize-Version -Value $existingVersion) -eq (Normalize-Version -Value $Version)) {
      Diagnose-Path -InstallDir $InstallDir -InstallPath $installPath
      Ok "$BinName $Version is already installed."
      exit 0
    }
  }
  else {
    Step "Found existing $BinName at $installPath"
  }
}
$tmpRoot = Join-Path ([System.IO.Path]::GetTempPath()) ("swobu-install-" + [System.Guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Force -Path $tmpRoot | Out-Null
$stagePath = Join-Path $InstallDir (".$BinName.new." + [System.Guid]::NewGuid().ToString('N') + '.exe')
$previousPath = $null

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

  Step "Staging $BinName in $InstallDir"
  Copy-Item -LiteralPath $sourceExe -Destination $stagePath
  Step 'Checking staged executable'
  Assert-BinaryVersion -Path $stagePath -ExpectedVersion $Version -Description 'staged executable'

  Step "Activating $installPath"
  if (-not $freshInstall) {
    $previousPath = Join-Path $InstallDir (".$BinName.previous." + [System.Guid]::NewGuid().ToString('N') + '.exe')
    Move-Item -LiteralPath $installPath -Destination $previousPath
  }
  try {
    Move-Item -LiteralPath $stagePath -Destination $installPath
  }
  catch {
    if ($previousPath -and (Test-Path -LiteralPath $previousPath) -and -not (Test-Path -LiteralPath $installPath)) {
      Restore-PreviousExecutable -PreviousPath $previousPath -InstallPath $installPath -ActivationError "failed to activate staged executable: $($_.Exception.Message)"
    }
    throw
  }

  try {
    Step 'Checking installed executable'
    Assert-BinaryVersion -Path $installPath -ExpectedVersion $Version -Description 'installed executable'
  }
  catch {
    if ($previousPath -and (Test-Path -LiteralPath $previousPath)) {
      Remove-Item -LiteralPath $installPath -Force -ErrorAction SilentlyContinue
      Restore-PreviousExecutable -PreviousPath $previousPath -InstallPath $installPath -ActivationError "installed executable verification failed: $($_.Exception.Message)"
    }
    else {
      Remove-Item -LiteralPath $installPath -Force -ErrorAction SilentlyContinue
    }
    throw
  }

  Ok "$BinName $Version installed"
  if ($previousPath) {
    Remove-Item -LiteralPath $previousPath -Force -ErrorAction SilentlyContinue
  }
  Get-ChildItem -LiteralPath $InstallDir -Filter ".$BinName.previous.*.exe" -File -ErrorAction SilentlyContinue | ForEach-Object {
    Remove-Item -LiteralPath $_.FullName -Force -ErrorAction SilentlyContinue
  }

  Diagnose-Path -InstallDir $InstallDir -InstallPath $installPath

  if ($freshInstall -and $StartSwobu) {
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
  elseif ($freshInstall) {
    Say ''
    Say 'Start Swobu:'
    Say "  $installPath"
  }
}
finally {
  if ($stagePath -and (Test-Path -LiteralPath $stagePath)) {
    Remove-Item -LiteralPath $stagePath -Force -ErrorAction SilentlyContinue
  }
  if (Test-Path -Path $tmpRoot) {
    Remove-Item -Path $tmpRoot -Recurse -Force -ErrorAction SilentlyContinue
  }
}

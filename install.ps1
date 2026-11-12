<#
.SYNOPSIS
  Install rex + rex-daemon on Windows (the install.sh counterpart).

.DESCRIPTION
  By default this downloads the latest GitHub release for Windows
  (rex_<version>_windows_amd64.zip), verifies it against the release's
  SHA256SUMS, and installs rex.exe and rex-daemon.exe into <Prefix>\bin
  (default: $HOME\.local\bin, the same layout as macOS/Linux). It does not
  edit your PATH or profile; it prints what to add when the folder is not
  on PATH. A running daemon is left running on the old binary, so your
  sessions keep going; the script prints how to switch it.

  -FromSource builds with the Go toolchain from this checkout instead (the
  old behavior): it stops a running daemon and adds <Prefix>\bin to the
  user PATH unless -NoPath is given.

  Requires Windows 10 1809+ (ConPTY and AF_UNIX sockets).

.EXAMPLE
  powershell -ExecutionPolicy Bypass -File .\install.ps1

.EXAMPLE
  .\install.ps1 -Version v2.0.0 -Prefix "$env:LOCALAPPDATA\Programs\rex"

.EXAMPLE
  .\install.ps1 -FromSource -NoPath
#>
[CmdletBinding()]
param(
  # Install root; binaries go to <Prefix>\bin.
  [string]$Prefix = (Join-Path $HOME '.local'),
  # Release to install (v2.0.0); default: $env:REX_VERSION, else the latest.
  [string]$Version = $env:REX_VERSION,
  # Build from this checkout with Go instead of downloading a release.
  [switch]$FromSource,
  # With -FromSource: copy existing .\rex.exe and .\rex-daemon.exe without
  # building (CI use).
  [switch]$SkipBuild,
  # With -FromSource: do not add <Prefix>\bin to the user PATH.
  [switch]$NoPath
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version 2
$Repo = 'TristanBietsch/rex'
$BinDir = Join-Path $Prefix 'bin'
$RepoRoot = $PSScriptRoot
# Largest file accepted from a release zip.
$MaxEntryBytes = 256MB

function Ok($msg)   { Write-Host "  $([char]0x2713) $msg" -ForegroundColor Green }
function Step($msg) { Write-Host "  -> $msg" -ForegroundColor DarkGray }
function Warn($msg) { Write-Host "  ! $msg" -ForegroundColor Yellow }
function Fail($msg) { Write-Host "  error: $msg" -ForegroundColor Red; exit 1 }

# ConPTY (pseudo consoles) landed in Windows 10 1809, build 17763.
$build = [Environment]::OSVersion.Version.Build
if ($build -lt 17763) {
  Fail "rex needs Windows 10 1809 (build 17763) or newer for ConPTY; this is build $build."
}
if ($SkipBuild) { $FromSource = $true }

# Run the given rex.exe with plain output; returns its exit code.
function Invoke-Rex([string]$Exe, [string[]]$Arguments) {
  $saved = $env:REX_AGENT
  $env:REX_AGENT = '0'
  try {
    & $Exe @Arguments *> $null
    return $LASTEXITCODE
  } catch {
    return 1
  } finally {
    $env:REX_AGENT = $saved
  }
}

# Windows locks a running .exe against overwrite but allows renaming it, so
# an open rex TUI or a running daemon does not block the upgrade. The old
# binary is moved to <name>.old (removed on the next install), then the new
# one, already copied next to it as <name>.new, is renamed into place.
function Install-Binary([string]$Source, [string]$Name) {
  $dest = Join-Path $BinDir $Name
  $new = "$dest.new"
  $old = "$dest.old"
  Copy-Item -LiteralPath $Source -Destination $new -Force
  if (Test-Path -LiteralPath $old) { Remove-Item -LiteralPath $old -Force -ErrorAction SilentlyContinue }
  if (Test-Path -LiteralPath $dest) {
    try { Move-Item -LiteralPath $dest -Destination $old -Force }
    catch {
      Remove-Item -LiteralPath $new -Force -ErrorAction SilentlyContinue
      Fail "cannot replace $dest ($_). Close rex and try again."
    }
  }
  try { Move-Item -LiteralPath $new -Destination $dest -Force }
  catch {
    if (Test-Path -LiteralPath $old) { Move-Item -LiteralPath $old -Destination $dest -Force -ErrorAction SilentlyContinue }
    Fail "cannot install $dest ($_)."
  }
}

# ---- release download -------------------------------------------------------

# Download Url to OutFile over https only (file:// for a local test mirror).
function Get-Download([string]$Url, [string]$OutFile) {
  if ($Url -like 'file://*') {
    Copy-Item -LiteralPath ([Uri]$Url).LocalPath -Destination $OutFile -Force
    return
  }
  if ($Url -notlike 'https://*') { Fail "refusing to download over a non-https URL: $Url" }
  $saved = $ProgressPreference
  $ProgressPreference = 'SilentlyContinue'  # the progress bar makes 5.1 very slow
  try {
    Invoke-WebRequest -Uri $Url -OutFile $OutFile -UseBasicParsing -MaximumRedirection 5
  } finally {
    $ProgressPreference = $saved
  }
}

function Install-Release {
  # Windows PowerShell 5.1 may default to TLS 1.0; GitHub needs 1.2+.
  try {
    [Net.ServicePointManager]::SecurityProtocol = [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12
  } catch { }
  try {
    [Net.ServicePointManager]::SecurityProtocol = [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls13
  } catch { }

  # REX_DOWNLOAD_URL points at a mirror of the release downloads (it must
  # have <url>/latest/download/<file> and <url>/download/<tag>/<file>).
  $downloadUrl = "https://github.com/$Repo/releases"
  if ($env:REX_DOWNLOAD_URL) { $downloadUrl = $env:REX_DOWNLOAD_URL.TrimEnd('/') }
  if ($downloadUrl -notmatch '^(https|file)://') {
    Fail "REX_DOWNLOAD_URL must start with https:// (got: $downloadUrl)"
  }

  # rex ships windows/amd64; Windows on ARM runs it under emulation.
  $arch = $env:PROCESSOR_ARCHITEW6432
  if (-not $arch) { $arch = $env:PROCESSOR_ARCHITECTURE }
  if ($arch -notin @('AMD64', 'ARM64')) {
    Fail "rex ships a 64-bit Windows build (detected $arch). Try -FromSource."
  }

  if ($Version) {
    $tag = $Version.Trim()
    if (-not $tag.StartsWith('v')) { $tag = "v$tag" }
    if ($tag -notmatch '^v[0-9A-Za-z.-]+$') { Fail "unexpected version: $Version" }
    $base = "$downloadUrl/download/$tag"
    $which = $tag
  } else {
    $base = "$downloadUrl/latest/download"
    $which = 'latest release'
  }

  $tmp = Join-Path ([IO.Path]::GetTempPath()) ("rex-install-" + [Guid]::NewGuid().ToString('N'))
  New-Item -ItemType Directory -Path $tmp | Out-Null
  try {
    Step "fetching checksums ($which)"
    $sumsFile = Join-Path $tmp 'SHA256SUMS'
    try { Get-Download "$base/SHA256SUMS" $sumsFile }
    catch { Fail "could not download $base/SHA256SUMS (no such release, or offline?): $_" }

    # The archive for Windows: rex_<ver>_windows_amd64.zip
    $pattern = '^([0-9a-fA-F]{64}) [ *](rex_[0-9A-Za-z.+-]+_windows_amd64\.zip)$'
    $entries = @(Get-Content -LiteralPath $sumsFile | ForEach-Object {
      if ($_ -match $pattern) { [pscustomobject]@{ Hash = $Matches[1].ToLowerInvariant(); File = $Matches[2] } }
    })
    if ($entries.Count -eq 0) { Fail 'the release has no Windows build. Try -FromSource.' }
    if ($entries.Count -gt 1) { Fail 'SHA256SUMS lists more than one Windows build; refusing to guess.' }
    $want = $entries[0].Hash
    $file = $entries[0].File
    $dir = $file.Substring(0, $file.Length - 4)  # strip .zip
    $label = 'v' + ($dir -replace '^rex_', '' -replace '_windows_amd64$', '')

    Step "downloading $file"
    $zipPath = Join-Path $tmp $file
    try { Get-Download "$base/$file" $zipPath }
    catch { Fail "could not download $base/${file}: $_" }

    $got = (Get-FileHash -LiteralPath $zipPath -Algorithm SHA256).Hash.ToLowerInvariant()
    if ($got -cne $want) {
      Fail "checksum mismatch for $file (want $want, got $got). Nothing was installed."
    }
    Step "checksum ok ($want)"

    # Extract only the two binaries, by exact name, from the zip's top
    # folder; nothing else in the archive is written anywhere.
    Add-Type -AssemblyName System.IO.Compression
    Add-Type -AssemblyName System.IO.Compression.FileSystem
    $stage = Join-Path $tmp 'stage'
    New-Item -ItemType Directory -Path $stage | Out-Null
    $zip = [IO.Compression.ZipFile]::OpenRead($zipPath)
    try {
      foreach ($name in 'rex.exe', 'rex-daemon.exe') {
        $found = @($zip.Entries | Where-Object { $_.FullName -ceq "$dir/$name" })
        if ($found.Count -ne 1) { Fail "$file does not contain $dir/$name" }
        if ($found[0].Length -gt $MaxEntryBytes) { Fail "$name in $file is too large" }
        [IO.Compression.ZipFileExtensions]::ExtractToFile($found[0], (Join-Path $stage $name), $true)
      }
    } finally {
      $zip.Dispose()
    }

    New-Item -ItemType Directory -Force -Path $BinDir | Out-Null
    Step "installing to $BinDir"
    # rex-daemon first, rex last (as install.sh).
    Install-Binary (Join-Path $stage 'rex-daemon.exe') 'rex-daemon.exe'
    Install-Binary (Join-Path $stage 'rex.exe') 'rex.exe'
    return $label
  } finally {
    Remove-Item -LiteralPath $tmp -Recurse -Force -ErrorAction SilentlyContinue
  }
}

# ---- source build (-FromSource) ---------------------------------------------

function Install-FromSource {
  if (-not $RepoRoot -or -not (Test-Path (Join-Path $RepoRoot 'go.mod'))) {
    Fail '-FromSource runs from a rex checkout (go.mod not found next to install.ps1)'
  }
  Set-Location $RepoRoot
  $label = (git -C $RepoRoot describe --tags --always 2>$null)
  if (-not $label) { $label = '(from source)' }

  if (-not $SkipBuild) {
    if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
      Fail 'go toolchain not found. Install the Go version in go.mod: https://go.dev/dl/'
    }
    # oto (TUI sounds) is pure Go on Windows; no cgo toolchain needed.
    $env:CGO_ENABLED = '0'
    $ld = "-X github.com/tristanbietsch/rex/internal/version.Version=$label"
    Step 'building rex'
    go build -trimpath -ldflags $ld -o rex.exe ./cmd/rex | Out-Host
    if ($LASTEXITCODE -ne 0) { Fail 'build of rex failed (see output above)' }
    Step 'building rex-daemon'
    go build -trimpath -ldflags $ld -o rex-daemon.exe ./cmd/rex-daemon | Out-Host
    if ($LASTEXITCODE -ne 0) { Fail 'build of rex-daemon failed (see output above)' }
  } elseif (-not ((Test-Path .\rex.exe) -and (Test-Path .\rex-daemon.exe))) {
    Fail '-SkipBuild set but .\rex.exe or .\rex-daemon.exe is missing'
  }

  # Stop a running daemon; the next `rex` starts it on the new binary.
  $rexForStop = @((Join-Path $BinDir 'rex.exe'), '.\rex.exe') | Where-Object { Test-Path $_ } | Select-Object -First 1
  if ($rexForStop -and (Invoke-Rex $rexForStop @('daemon', 'status')) -eq 0) {
    Step 'stopping running daemon'
    Invoke-Rex $rexForStop @('daemon', 'stop') | Out-Null
    $script:DaemonStopped = $true
  }

  New-Item -ItemType Directory -Force -Path $BinDir | Out-Null
  Step "installing to $BinDir"
  Install-Binary (Join-Path $RepoRoot 'rex-daemon.exe') 'rex-daemon.exe'
  Install-Binary (Join-Path $RepoRoot 'rex.exe') 'rex.exe'

  $userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
  $onUserPath = ($userPath -split ';' | Where-Object { $_ -and ($_.TrimEnd('\') -ieq $BinDir.TrimEnd('\')) })
  if (-not $onUserPath -and -not $NoPath) {
    $newPath = if ($userPath) { "$userPath;$BinDir" } else { $BinDir }
    [Environment]::SetEnvironmentVariable('Path', $newPath, 'User')
    $env:Path = "$env:Path;$BinDir"
    Step "added $BinDir to your user PATH (open a new terminal to pick it up)"
  }
  return $label
}

# ---- main -------------------------------------------------------------------

Write-Host ''
Write-Host '  rex installer'
Write-Host ''

$script:DaemonStopped = $false
$installedRex = Join-Path $BinDir 'rex.exe'
# Was a daemon running before the swap? A release install leaves it running.
$daemonWasRunning = (Test-Path -LiteralPath $installedRex) -and ((Invoke-Rex $installedRex @('daemon', 'status')) -eq 0)

if ($FromSource) { $label = Install-FromSource } else { $label = Install-Release }

$installed = $null
try { $installed = (& $installedRex --version 2>$null) } catch { }
if (-not $installed) { $installed = $label }
Write-Host ''
Ok "rex $installed installed"
Write-Host "    $installedRex" -ForegroundColor DarkGray
Write-Host "    $(Join-Path $BinDir 'rex-daemon.exe')" -ForegroundColor DarkGray

$onPath = ($env:Path -split ';' | Where-Object { $_ -and ($_.TrimEnd('\') -ieq $BinDir.TrimEnd('\')) })
if (-not $onPath) {
  Write-Host ''
  Warn "$BinDir is not on PATH. To add it for your user, run:"
  Write-Host "    [Environment]::SetEnvironmentVariable('Path', [Environment]::GetEnvironmentVariable('Path', 'User') + ';$BinDir', 'User')" -ForegroundColor DarkGray
  Write-Host '    then open a new terminal.' -ForegroundColor DarkGray
}

if ($script:DaemonStopped) {
  Write-Host ''
  Write-Host '  run rex - the daemon will restart on the new binary' -ForegroundColor DarkGray
} elseif ($daemonWasRunning) {
  Write-Host ''
  Warn 'the running daemon still uses the old binary.'
  if ((Invoke-Rex $installedRex @('help', 'upgrade')) -eq 0) {
    Write-Host '    run rex upgrade to switch without ending sessions' -ForegroundColor DarkGray
  } else {
    Write-Host '    run rex daemon restart when your sessions are done (it ends them)' -ForegroundColor DarkGray
  }
}
Write-Host ''
if (-not (Test-Path (Join-Path $HOME '.config\rex'))) {
  Write-Host '  next: run rex setup to configure rex'
} else {
  Write-Host '  next: run rex to launch'
}
Write-Host ''
exit 0

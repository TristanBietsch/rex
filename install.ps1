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
$MaxEntryBytes = 128MB

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

# Run the given rex.exe with plain output; returns its exit code. Native
# stderr must not turn into a terminating error under 'Stop' (5.1 does that).
function Invoke-Rex([string]$Exe, [string[]]$Arguments) {
  $ErrorActionPreference = 'Continue'
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

# Install rex-daemon.exe then rex.exe from $SourceDir into $BinDir.
# Windows locks a running .exe against overwrite but allows renaming it, so
# an open rex TUI or a running daemon does not block the upgrade: each old
# binary is moved aside to <name>.old (or a unique .old name when a stale
# one is still locked), then the new one, copied next to it under a unique
# name, is renamed into place. Every copy and move-aside happens before the
# first swap, and a failed swap puts the old binaries back, so a failure
# never leaves a new rex-daemon next to an old rex.
function Install-Binaries([string]$SourceDir) {
  $names = 'rex-daemon.exe', 'rex.exe'
  $staged = @{}
  $aside = @{}
  $id = [Guid]::NewGuid().ToString('N')
  try {
    foreach ($n in $names) {
      $new = Join-Path $BinDir ".$n.$id.new"
      Copy-Item -LiteralPath (Join-Path $SourceDir $n) -Destination $new
      $staged[$n] = $new
    }
    foreach ($n in $names) {
      $dest = Join-Path $BinDir $n
      if (-not (Test-Path -LiteralPath $dest)) { continue }
      $old = "$dest.old"
      if (Test-Path -LiteralPath $old) {
        try { Remove-Item -LiteralPath $old -Force } catch { $old = "$dest.$id.old" }
      }
      Move-Item -LiteralPath $dest -Destination $old
      $aside[$n] = $old
    }
    foreach ($n in $names) {
      Move-Item -LiteralPath $staged[$n] -Destination (Join-Path $BinDir $n)
      $staged.Remove($n)
    }
  } catch {
    $err = $_
    foreach ($n in $names) {
      $dest = Join-Path $BinDir $n
      if ($aside.ContainsKey($n)) {
        if (Test-Path -LiteralPath $dest) { Remove-Item -LiteralPath $dest -Force -ErrorAction SilentlyContinue }
        Move-Item -LiteralPath $aside[$n] -Destination $dest -ErrorAction SilentlyContinue
      }
    }
    foreach ($f in $staged.Values) { Remove-Item -LiteralPath $f -Force -ErrorAction SilentlyContinue }
    Fail "cannot install into $BinDir ($err). Close rex and try again; the old binaries were kept."
  }
}

# ---- release download -------------------------------------------------------

# Hosts downloads and their redirects may go to.
$AllowedHosts = @('github.com', 'api.github.com', 'objects.githubusercontent.com', 'release-assets.githubusercontent.com')
$MaxDownloadBytes = 256MB

# Download Url to OutFile: https only, every redirect checked against
# $AllowedHosts (plus the REX_DOWNLOAD_URL host), at most $MaxBytes.
# file:// is allowed for a local test mirror.
function Get-Download([string]$Url, [string]$OutFile, [long]$MaxBytes = $MaxDownloadBytes) {
  if ($Url -like 'file://*') {
    $src = ([Uri]$Url).LocalPath
    if ((Get-Item -LiteralPath $src).Length -gt $MaxBytes) { throw "$Url is too large" }
    Copy-Item -LiteralPath $src -Destination $OutFile -Force
    return
  }
  Add-Type -AssemblyName System.Net.Http
  $handler = New-Object System.Net.Http.HttpClientHandler
  $handler.AllowAutoRedirect = $false
  $client = New-Object System.Net.Http.HttpClient($handler)
  $client.Timeout = [TimeSpan]::FromMinutes(10)
  $client.DefaultRequestHeaders.UserAgent.ParseAdd('rex-installer')
  try {
    $uri = [Uri]$Url
    for ($hop = 0; ; $hop++) {
      if ($uri.Scheme -ne 'https') { throw "refusing non-https URL $uri" }
      if ($script:AllowedHosts -notcontains $uri.Host -or -not $uri.IsDefaultPort) {
        throw "$($uri.Authority) is not an allowed download host"
      }
      $resp = $client.GetAsync($uri, [System.Net.Http.HttpCompletionOption]::ResponseHeadersRead).GetAwaiter().GetResult()
      $code = [int]$resp.StatusCode
      if ($code -ge 300 -and $code -lt 400 -and $resp.Headers.Location) {
        if ($hop -ge 5) { throw 'too many redirects' }
        $loc = $resp.Headers.Location
        if (-not $loc.IsAbsoluteUri) { $loc = New-Object Uri($uri, $loc) }
        $resp.Dispose()
        $uri = $loc
        continue
      }
      if ($code -ne 200) { throw "GET $uri : $code $($resp.ReasonPhrase)" }
      $len = $resp.Content.Headers.ContentLength
      if ($len -and $len -gt $MaxBytes) { throw "$uri is too large ($len bytes)" }
      $in = $resp.Content.ReadAsStreamAsync().GetAwaiter().GetResult()
      $out = [IO.File]::Open($OutFile, [IO.FileMode]::CreateNew)
      try {
        $buf = New-Object byte[] 81920
        $total = 0
        while (($n = $in.Read($buf, 0, $buf.Length)) -gt 0) {
          $total += $n
          if ($total -gt $MaxBytes) { throw "$uri is too large (over $MaxBytes bytes)" }
          $out.Write($buf, 0, $n)
        }
      } finally {
        $out.Dispose(); $in.Dispose(); $resp.Dispose()
      }
      return
    }
  } finally {
    $client.Dispose()
  }
}

# Copy one zip entry to Path, reading at most $Max bytes whatever its
# header claims.
function Expand-Entry($Entry, [string]$Path, [long]$Max) {
  $in = $Entry.Open()
  $out = [IO.File]::Open($Path, [IO.FileMode]::CreateNew)
  try {
    $buf = New-Object byte[] 81920
    $total = 0
    while (($n = $in.Read($buf, 0, $buf.Length)) -gt 0) {
      $total += $n
      if ($total -gt $Max) { throw "$($Entry.FullName) is too large" }
      $out.Write($buf, 0, $n)
    }
  } finally {
    $out.Dispose(); $in.Dispose()
  }
}

function Install-Release {
  # Windows PowerShell 5.1 may default to TLS 1.0; allow 1.2 (and 1.3
  # where the OS has it) and nothing older.
  $proto = [Net.SecurityProtocolType]::Tls12
  try { $proto = $proto -bor [Net.SecurityProtocolType]::Tls13 } catch { }
  try { [Net.ServicePointManager]::SecurityProtocol = $proto } catch { }

  # REX_DOWNLOAD_URL points at a mirror of the release downloads (it must
  # have <url>/latest/download/<file> and <url>/download/<tag>/<file>).
  $downloadUrl = "https://github.com/$Repo/releases"
  if ($env:REX_DOWNLOAD_URL) { $downloadUrl = $env:REX_DOWNLOAD_URL.TrimEnd('/') }
  if ($downloadUrl -notmatch '^(https|file)://') {
    Fail "REX_DOWNLOAD_URL must start with https:// (got: $downloadUrl)"
  }
  # A mirror is trusted fully: it serves both the zip and its checksum.
  if ($downloadUrl -like 'https://*') { $script:AllowedHosts += ([Uri]$downloadUrl).Host }

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
    if ($Version -and $file -cne "rex_$($tag.Substring(1))_windows_amd64.zip") {
      Fail "SHA256SUMS of $tag lists $file, not the build for $tag"
    }
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
        try { Expand-Entry $found[0] (Join-Path $stage $name) $MaxEntryBytes }
        catch { Fail "${file}: $_" }
      }
    } finally {
      $zip.Dispose()
    }

    New-Item -ItemType Directory -Force -Path $BinDir | Out-Null
    Step "installing to $BinDir"
    Install-Binaries $stage
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
  Install-Binaries $RepoRoot

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

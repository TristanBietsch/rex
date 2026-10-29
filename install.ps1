<#
.SYNOPSIS
  Build and install rex + rex-daemon on Windows (the install.sh counterpart).

.DESCRIPTION
  Builds rex.exe and rex-daemon.exe with the Go toolchain and copies them into
  <Prefix>\bin (default: $HOME\.local\bin, the same layout as macOS/Linux).
  If a daemon is running it is stopped first and left stopped; the next `rex`
  starts it again on the new binary. The bin directory is added to the user
  PATH unless -NoPath is given.

  Requires Windows 10 1809+ (ConPTY and AF_UNIX sockets) and Go 1.26+.

.EXAMPLE
  powershell -ExecutionPolicy Bypass -File .\install.ps1

.EXAMPLE
  .\install.ps1 -Prefix "$env:LOCALAPPDATA\Programs\rex" -NoPath
#>
[CmdletBinding()]
param(
  # Install root; binaries go to <Prefix>\bin.
  [string]$Prefix = (Join-Path $HOME '.local'),
  # Copy existing .\rex.exe and .\rex-daemon.exe without building (CI use).
  [switch]$SkipBuild,
  # Do not add <Prefix>\bin to the user PATH.
  [switch]$NoPath
)

$ErrorActionPreference = 'Stop'
$BinDir = Join-Path $Prefix 'bin'
$RepoRoot = $PSScriptRoot
Set-Location $RepoRoot

function Ok($msg)   { Write-Host "  $([char]0x2713) $msg" -ForegroundColor Green }
function Step($msg) { Write-Host "  -> $msg" -ForegroundColor DarkGray }
function Fail($msg) { Write-Host "  error: $msg" -ForegroundColor Red; exit 1 }

# ConPTY (pseudo consoles) landed in Windows 10 1809, build 17763.
$build = [Environment]::OSVersion.Version.Build
if ($build -lt 17763) {
  Fail "rex needs Windows 10 1809 (build 17763) or newer for ConPTY; this is build $build."
}

# git is optional (e.g. a source zip). Outside a checkout git writes to
# stderr, which Windows PowerShell 5.1 turns into a terminating error under
# ErrorActionPreference 'Stop', hence the try.
$gitVersion = $null
if (Get-Command git -ErrorAction SilentlyContinue) {
  try { $gitVersion = (git -C $RepoRoot describe --tags --always --dirty 2>$null) } catch { $gitVersion = $null }
}
$version = if ($gitVersion) { $gitVersion } else { '(from source)' }
# Stamp `rex --version`; without git the binary keeps its built-in version.
# An args array, not an empty -ldflags '': Windows PowerShell 5.1 drops empty
# arguments to native commands.
$ldflagArgs = @()
if ($gitVersion) {
  $ldflagArgs = @('-ldflags', "-X github.com/tristanbietsch/rex/internal/surface/cli/core.Version=$gitVersion")
}
Write-Host ""
Write-Host "  rex installer  $version"
Write-Host ""

# ---- build ------------------------------------------------------------------
if (-not $SkipBuild) {
  if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
    Fail "go toolchain not found. Install Go 1.26+: https://go.dev/dl/"
  }
  # oto (TUI sounds) is pure Go on Windows; no cgo toolchain needed.
  $env:CGO_ENABLED = '0'
  Step 'building rex'
  go build @ldflagArgs -o rex.exe ./cmd/rex
  if ($LASTEXITCODE -ne 0) { Fail 'build of rex failed (see output above)' }
  Step 'building rex-daemon'
  go build @ldflagArgs -o rex-daemon.exe ./cmd/rex-daemon
  if ($LASTEXITCODE -ne 0) { Fail 'build of rex-daemon failed (see output above)' }
} elseif (-not ((Test-Path .\rex.exe) -and (Test-Path .\rex-daemon.exe))) {
  Fail '-SkipBuild set but .\rex.exe or .\rex-daemon.exe is missing'
}

# ---- stop a running daemon --------------------------------------------------
$daemonWasRunning = $false
$rexForStop = @((Join-Path $BinDir 'rex.exe'), '.\rex.exe') | Where-Object { Test-Path $_ } | Select-Object -First 1
if ($rexForStop) {
  & $rexForStop daemon status *> $null
  if ($LASTEXITCODE -eq 0) {
    Step 'stopping running daemon'
    & $rexForStop daemon stop *> $null
    $daemonWasRunning = $true
  }
}

# ---- install ----------------------------------------------------------------
New-Item -ItemType Directory -Force -Path $BinDir | Out-Null
Step "installing to $BinDir"
foreach ($exe in 'rex.exe', 'rex-daemon.exe') {
  $dest = Join-Path $BinDir $exe
  # Windows locks a running .exe against overwrite but allows renaming it, so
  # an open rex TUI does not block the upgrade. The .old copy is cleaned up on
  # the next install.
  $old = "$dest.old"
  if (Test-Path $old) { Remove-Item $old -Force -ErrorAction SilentlyContinue }
  if (Test-Path $dest) {
    try { Move-Item $dest $old -Force } catch { Fail "cannot replace $dest ($_). Close rex and try again." }
  }
  Copy-Item (Join-Path $RepoRoot $exe) $dest -Force
}

# ---- PATH -------------------------------------------------------------------
$userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
$onPath = ($userPath -split ';' | Where-Object { $_ -and ($_.TrimEnd('\') -ieq $BinDir.TrimEnd('\')) })
if (-not $onPath) {
  if ($NoPath) {
    Write-Host ""
    Write-Host "  ! $BinDir is not on PATH" -ForegroundColor Yellow
  } else {
    $newPath = if ($userPath) { "$userPath;$BinDir" } else { $BinDir }
    [Environment]::SetEnvironmentVariable('Path', $newPath, 'User')
    $env:Path = "$env:Path;$BinDir"
    Step "added $BinDir to your user PATH (open a new terminal to pick it up)"
  }
}

# ---- done -------------------------------------------------------------------
$installed = (& (Join-Path $BinDir 'rex.exe') --version 2>$null)
if (-not $installed) { $installed = '?' }
Write-Host ""
Ok "rex $installed installed"
Write-Host "    $(Join-Path $BinDir 'rex.exe')" -ForegroundColor DarkGray
Write-Host "    $(Join-Path $BinDir 'rex-daemon.exe')" -ForegroundColor DarkGray
if ($daemonWasRunning) {
  Write-Host ""
  Write-Host "  run rex - the daemon will restart on the new binary" -ForegroundColor DarkGray
}
Write-Host ""
if (-not (Test-Path (Join-Path $HOME '.config\rex'))) {
  Write-Host "  next: run rex setup to configure rex"
} else {
  Write-Host "  next: run rex to launch"
}
Write-Host ""

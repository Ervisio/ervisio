<#
.SYNOPSIS
  Installs, upgrades or removes the Ervisio service on Windows.

.DESCRIPTION
  Run from an elevated PowerShell, from the folder that holds ervisiod.exe,
  ervisio-bridge.exe and web\ (the Windows release archive):

    .\install.ps1                 # install or upgrade
    .\install.ps1 -Uninstall      # remove service, firewall rule, program files
    .\install.ps1 -Uninstall -PurgeData   # also delete %ProgramData%\Ervisio

  Layout:
    %ProgramFiles%\Ervisio           ervisiod.exe, ervisio-bridge.exe, web\, plugins\
    %ProgramData%\Ervisio            ervisio.conf, tls\ (self-signed certificate,
                                     created by ervisiod on first start), data\,
                                     run\, logs\   (SYSTEM + Administrators only)
#>
[CmdletBinding()]
param(
    [switch]$Uninstall,
    [switch]$PurgeData,
    [string]$SourceDir = $PSScriptRoot,
    [int]$Port = 9090,
    [string]$InstallDir = (Join-Path $env:ProgramFiles 'Ervisio')
)
$ErrorActionPreference = 'Stop'

$ServiceName = 'Ervisio'
$RuleName = 'Ervisio (TCP)'
$DataDir = Join-Path $env:ProgramData 'Ervisio'

$principal = [Security.Principal.WindowsPrincipal][Security.Principal.WindowsIdentity]::GetCurrent()
if (-not $principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) {
    throw 'Run this script from an elevated (Administrator) PowerShell.'
}

function Stop-ErvisioService {
    $svc = Get-Service -Name $ServiceName -ErrorAction SilentlyContinue
    if ($svc -and $svc.Status -ne 'Stopped') {
        Stop-Service -Name $ServiceName -Force
        $svc.WaitForStatus('Stopped', [TimeSpan]::FromSeconds(40))
    }
}

function Remove-ErvisioFirewallRule {
    & netsh.exe advfirewall firewall delete rule name="$RuleName" 2>&1 | Out-Null
}

if ($Uninstall) {
    Stop-ErvisioService
    if (Get-Service -Name $ServiceName -ErrorAction SilentlyContinue) {
        & sc.exe delete $ServiceName | Out-Null
    }
    Remove-ErvisioFirewallRule
    if (Test-Path $InstallDir) { Remove-Item -Recurse -Force $InstallDir }
    if ($PurgeData -and (Test-Path $DataDir)) { Remove-Item -Recurse -Force $DataDir }
    elseif (Test-Path $DataDir) { Write-Host "Kept $DataDir (configuration, certificate, data). Use -PurgeData to delete it." }
    Write-Host 'Ervisio removed.'
    return
}

foreach ($f in 'ervisiod.exe', 'ervisio-bridge.exe') {
    if (-not (Test-Path (Join-Path $SourceDir $f))) { throw "$f not found in $SourceDir" }
}

# Program files (stop the service first so the binaries can be replaced).
Stop-ErvisioService
New-Item -ItemType Directory -Force -Path $InstallDir | Out-Null
foreach ($f in 'ervisiod.exe', 'ervisio-bridge.exe') {
    Copy-Item -Force (Join-Path $SourceDir $f) $InstallDir
}
foreach ($d in 'web', 'plugins') {
    $src = Join-Path $SourceDir $d
    if (Test-Path $src) {
        $dst = Join-Path $InstallDir $d
        if (Test-Path $dst) { Remove-Item -Recurse -Force $dst }
        Copy-Item -Recurse -Force $src $dst
    }
}

# Data folder: only SYSTEM and Administrators (holds the TLS key and secrets).
New-Item -ItemType Directory -Force -Path $DataDir | Out-Null
foreach ($d in 'data', 'run', 'logs', 'tls') {
    New-Item -ItemType Directory -Force -Path (Join-Path $DataDir $d) | Out-Null
}
# S-1-5-18 = SYSTEM, S-1-5-32-544 = Administrators (language independent).
& icacls.exe $DataDir /inheritance:r /grant:r '*S-1-5-18:(OI)(CI)F' '*S-1-5-32-544:(OI)(CI)F' | Out-Null
if ($LASTEXITCODE -ne 0) { throw "icacls failed ($LASTEXITCODE)" }

# Default configuration (never overwritten on upgrade).
$conf = Join-Path $DataDir 'ervisio.conf'
if (-not (Test-Path $conf)) {
    Set-Content -Path $conf -Encoding UTF8 -Value "listen = `"0.0.0.0:$Port`"`r`n"
}

# Service.
$exe = Join-Path $InstallDir 'ervisiod.exe'
$binPath = "`"$exe`""
if (Get-Service -Name $ServiceName -ErrorAction SilentlyContinue) {
    & sc.exe config $ServiceName binPath= $binPath start= auto obj= LocalSystem | Out-Null
} else {
    & sc.exe create $ServiceName binPath= $binPath start= auto obj= LocalSystem DisplayName= 'Ervisio web console' | Out-Null
}
if ($LASTEXITCODE -ne 0) { throw "sc.exe failed ($LASTEXITCODE)" }
& sc.exe description $ServiceName 'Ervisio web administration console' | Out-Null
& sc.exe failure $ServiceName reset= 86400 actions= restart/5000/restart/30000// | Out-Null

# Firewall.
Remove-ErvisioFirewallRule
& netsh.exe advfirewall firewall add rule name="$RuleName" dir=in action=allow protocol=TCP localport=$Port program="$exe" enable=yes profile=any | Out-Null
if ($LASTEXITCODE -ne 0) { throw "netsh failed ($LASTEXITCODE)" }

Start-Service -Name $ServiceName
Write-Host "Ervisio is running: https://localhost:$Port (self-signed certificate, created on first start in $DataDir\tls)."
Write-Host "Configuration: $conf"

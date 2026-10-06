<#
.SYNOPSIS
  One-command install of Ervisio on Windows.

.DESCRIPTION
  From an elevated (Administrator) PowerShell:

    irm https://raw.githubusercontent.com/ervisio/ervisio/main/install-windows.ps1 | iex

  Downloads the latest Ervisio release for this machine (amd64 or arm64) from
  GitHub, checks the archive against the release's SHA256SUMS, extracts it and
  runs its install.ps1, which installs the Ervisio service, keeps
  configuration and data in %ProgramData%\Ervisio and opens port 9090.
  Running it again upgrades and keeps the configuration.

  With options (a specific version, another port, or removal):

    & ([scriptblock]::Create((irm https://raw.githubusercontent.com/ervisio/ervisio/main/install-windows.ps1))) -Version 1.2.3 -Port 8443
    & ([scriptblock]::Create((irm https://raw.githubusercontent.com/ervisio/ervisio/main/install-windows.ps1))) -Uninstall

  The archive's checksum is verified; the ed25519 signature on SHA256SUMS is
  not checked here yet (install.sh on Linux does check it).
#>
[CmdletBinding()]
param(
    [string]$Version = '',
    [int]$Port = 9090,
    [switch]$Uninstall,
    [switch]$PurgeData
)
$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'   # Invoke-WebRequest is much faster without it

$Repo = 'ervisio/ervisio'

$principal = New-Object Security.Principal.WindowsPrincipal([Security.Principal.WindowsIdentity]::GetCurrent())
if (-not $principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) {
    throw 'Run this from an elevated (Administrator) PowerShell.'
}
[Net.ServicePointManager]::SecurityProtocol = [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12

if ($Uninstall) {
    $installed = Join-Path $env:ProgramFiles 'Ervisio\install.ps1'
    if (-not (Test-Path $installed)) { throw "Ervisio is not installed (no $installed)." }
    & $installed -Uninstall -PurgeData:$PurgeData
    return
}

$arch = switch ($env:PROCESSOR_ARCHITECTURE) {
    'AMD64' { 'amd64' }
    'ARM64' { 'arm64' }
    default { throw "Unsupported processor architecture: $env:PROCESSOR_ARCHITECTURE" }
}

$headers = @{ 'User-Agent' = 'ervisio-install-windows' }
if ($Version) {
    $tag = 'v' + $Version.TrimStart('v')
    $release = Invoke-RestMethod "https://api.github.com/repos/$Repo/releases/tags/$tag" -Headers $headers
} else {
    $release = Invoke-RestMethod "https://api.github.com/repos/$Repo/releases/latest" -Headers $headers
}
$ver = $release.tag_name.TrimStart('v')
$zipName = "ervisio-$ver-windows-$arch.zip"
$zipAsset = $release.assets | Where-Object name -EQ $zipName
$sumAsset = $release.assets | Where-Object name -EQ 'SHA256SUMS'
if (-not $zipAsset) { throw "Release $($release.tag_name) has no $zipName (Windows builds start with the first release that includes them)." }
if (-not $sumAsset) { throw "Release $($release.tag_name) has no SHA256SUMS." }

$work = Join-Path ([IO.Path]::GetTempPath()) ("ervisio-install-" + [guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $work | Out-Null
try {
    Write-Host "Downloading Ervisio $ver ($arch)..."
    $zip = Join-Path $work $zipName
    $sums = Join-Path $work 'SHA256SUMS'
    Invoke-WebRequest $zipAsset.browser_download_url -OutFile $zip -Headers $headers -UseBasicParsing
    Invoke-WebRequest $sumAsset.browser_download_url -OutFile $sums -Headers $headers -UseBasicParsing

    $line = Get-Content $sums | Where-Object { ($_ -split '\s+', 2)[1] -replace '^\*', '' -eq $zipName } | Select-Object -First 1
    if (-not $line) { throw "SHA256SUMS does not list $zipName." }
    $want = ($line -split '\s+')[0].ToLowerInvariant()
    $got = (Get-FileHash $zip -Algorithm SHA256).Hash.ToLowerInvariant()
    if ($got -ne $want) { throw "Checksum mismatch for ${zipName}: got $got, expected $want." }
    Write-Host 'Checksum OK.'

    Expand-Archive $zip -DestinationPath $work -Force
    $src = Join-Path $work "ervisio-$ver-windows-$arch"
    & (Join-Path $src 'install.ps1') -SourceDir $src -Port $Port

    # Keep install.ps1 next to the program, so -Uninstall works later.
    Copy-Item (Join-Path $src 'install.ps1') (Join-Path $env:ProgramFiles 'Ervisio\install.ps1') -Force

    $ips = Get-NetIPAddress -AddressFamily IPv4 -ErrorAction SilentlyContinue |
        Where-Object { $_.IPAddress -notlike '127.*' -and $_.IPAddress -notlike '169.254.*' } |
        Select-Object -ExpandProperty IPAddress
    Write-Host ''
    Write-Host "Ervisio $ver is installed. Open:"
    Write-Host "  https://localhost:$Port"
    foreach ($ip in $ips) { Write-Host "  https://${ip}:$Port" }
    Write-Host 'Sign in with a Windows account of this machine. The certificate is self-signed.'
} finally {
    Remove-Item $work -Recurse -Force -ErrorAction SilentlyContinue
}

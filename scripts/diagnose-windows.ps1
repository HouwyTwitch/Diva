[CmdletBinding()]
param(
    [string]$FFmpeg = 'C:\ffmpeg\bin\ffmpeg.exe'
)

$identity = [Security.Principal.WindowsIdentity]::GetCurrent()
$principal = [Security.Principal.WindowsPrincipal]::new($identity)
$isElevated = $principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)
$installer = Join-Path $PSScriptRoot 'install-agent.ps1'
$versionLine = Select-String -Path $installer -Pattern '^# Diva-Installer-Version:' -ErrorAction SilentlyContinue | Select-Object -First 1

Write-Host '=== Diva Windows diagnostics ===' -ForegroundColor Cyan
Write-Host "User:              $($identity.Name)"
Write-Host "PowerShell:        $($PSVersionTable.PSVersion) ($($PSVersionTable.PSEdition))"
Write-Host "Elevated token:    $isElevated"
Write-Host "Installer path:    $installer"
Write-Host "Installer version: $(if ($versionLine) { ($versionLine.Line -split ':', 2)[1].Trim() } else { 'LEGACY/UNKNOWN - update the repository' })"
Write-Host "Agent built:       $(Test-Path (Join-Path $PSScriptRoot '..\dist\diva-agent.exe'))"
Write-Host "FFmpeg found:      $(Test-Path $FFmpeg) ($FFmpeg)"

$task = Get-ScheduledTask -TaskName 'Diva Remote Agent' -ErrorAction SilentlyContinue
Write-Host "Scheduled task:    $([bool]$task)"
$firewall = Get-NetFirewallRule -DisplayName 'Diva WebRTC UDP' -ErrorAction SilentlyContinue
Write-Host "Firewall rule:     $([bool]$firewall)"

if (-not $versionLine) {
    Write-Warning 'The installer is an old copy. Run git pull --ff-only or download a fresh repository archive.'
}
if (-not $isElevated) {
    Write-Host 'This shell has a filtered token. Installer v2 will request UAC automatically.' -ForegroundColor Yellow
}

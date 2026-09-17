$ErrorActionPreference = 'Stop'
if (-not ([Security.Principal.WindowsPrincipal][Security.Principal.WindowsIdentity]::GetCurrent()).IsInRole('Administrator')) { throw 'Run PowerShell as Administrator.' }
Stop-ScheduledTask -TaskName 'Diva Remote Agent' -ErrorAction SilentlyContinue
Unregister-ScheduledTask -TaskName 'Diva Remote Agent' -Confirm:$false -ErrorAction SilentlyContinue
Remove-NetFirewallRule -DisplayName 'Diva WebRTC UDP' -ErrorAction SilentlyContinue
Remove-Item "$env:LOCALAPPDATA\Diva" -Recurse -Force -ErrorAction SilentlyContinue
Write-Host 'Diva agent removed.' -ForegroundColor Green

[CmdletBinding()]
param(
    [Parameter(Mandatory)][string]$Server,
    [Parameter(Mandatory)][string]$Room,
    [Parameter(Mandatory)][string]$Token,
    [Parameter(Mandatory)][string]$PublicIP,
    [string]$Agent = "$PSScriptRoot\..\dist\diva-agent.exe",
    [string]$FFmpeg = "C:\ffmpeg\bin\ffmpeg.exe",
    [ValidateSet('libx264','h264_nvenc','h264_qsv','h264_amf')][string]$Encoder = 'h264_nvenc',
    [ValidateRange(1,240)][int]$FPS = 60,
    [ValidateRange(500,100000)][int]$Bitrate = 20000,
    [ValidateRange(1024,65535)][int]$UDPPort = 50000,
    [int]$X = 0, [int]$Y = 0, [int]$Width = 0, [int]$Height = 0
)
$ErrorActionPreference = 'Stop'
if (-not ([Security.Principal.WindowsPrincipal][Security.Principal.WindowsIdentity]::GetCurrent()).IsInRole('Administrator')) {
    throw 'Run PowerShell as Administrator.'
}
if (-not (Test-Path $Agent)) { throw "Agent not found: $Agent. Run scripts\build-agent.ps1 first." }
if (-not (Test-Path $FFmpeg)) { throw "FFmpeg not found: $FFmpeg" }
$dir = Join-Path $env:LOCALAPPDATA 'Diva'
New-Item -ItemType Directory -Force $dir | Out-Null
Copy-Item $Agent "$dir\diva-agent.exe" -Force
$escapedToken = $Token.Replace('"','\"')
$arguments = @('-server', "`"$Server`"", '-room', "`"$Room`"", '-token', "`"$escapedToken`"", '-public-ip', $PublicIP, '-udp-port', $UDPPort, '-ffmpeg', "`"$FFmpeg`"", '-encoder', $Encoder, '-fps', $FPS, '-bitrate', $Bitrate, '-x', $X, '-y', $Y, '-width', $Width, '-height', $Height) -join ' '
$action = New-ScheduledTaskAction -Execute "$dir\diva-agent.exe" -Argument $arguments
$trigger = New-ScheduledTaskTrigger -AtLogOn -User $env:USERNAME
$principal = New-ScheduledTaskPrincipal -UserId "$env:USERDOMAIN\$env:USERNAME" -LogonType Interactive -RunLevel Highest
$settings = New-ScheduledTaskSettingsSet -RestartCount 99 -RestartInterval (New-TimeSpan -Minutes 1) -ExecutionTimeLimit ([TimeSpan]::Zero)
Register-ScheduledTask -TaskName 'Diva Remote Agent' -Action $action -Trigger $trigger -Principal $principal -Settings $settings -Force | Out-Null
New-NetFirewallRule -DisplayName 'Diva WebRTC UDP' -Direction Inbound -Action Allow -Protocol UDP -LocalPort $UDPPort -Profile Any -ErrorAction SilentlyContinue | Out-Null
Start-ScheduledTask -TaskName 'Diva Remote Agent'
Write-Host "Diva installed and started. UDP $UDPPort is open in Windows Firewall." -ForegroundColor Green
Write-Warning 'The agent captures only an unlocked interactive desktop. Locking Windows stops useful capture/input.'

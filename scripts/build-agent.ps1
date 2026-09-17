[CmdletBinding()]
param([string]$Output = "$PSScriptRoot\..\dist\diva-agent.exe")
$ErrorActionPreference = 'Stop'
$root = (Resolve-Path "$PSScriptRoot\..").Path
New-Item -ItemType Directory -Force (Split-Path $Output) | Out-Null
Push-Location $root
try {
    go mod download
    $env:CGO_ENABLED = '0'
    go build -trimpath -ldflags '-s -w' -o $Output ./cmd/agent
    Write-Host "Built $Output" -ForegroundColor Green
} finally { Pop-Location }

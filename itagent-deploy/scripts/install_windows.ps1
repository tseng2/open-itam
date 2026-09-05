param (
    [Parameter(Mandatory=$true)][string]$InstallToken,
    [string]$Server = "http://127.0.0.1:8443",
    [string]$Password = "Admin@12345",
    [string]$InstallDir = "C:\ProgramData\ITAgent"
)

if (-NOT ([Security.Principal.WindowsPrincipal][Security.Principal.WindowsIdentity]::GetCurrent()).IsInRole([Security.Principal.WindowsBuiltInRole]"Administrator")) {
    Write-Host "ERROR: Please run as Administrator" -ForegroundColor Red
    exit 1
}

Write-Host "=== IT Agent Install ===" -ForegroundColor Green

Get-Process core-agent -ErrorAction SilentlyContinue | Stop-Process -Force
Get-Process tray -ErrorAction SilentlyContinue | Stop-Process -Force
Get-Process agent-watchdog -ErrorAction SilentlyContinue | Stop-Process -Force

New-Item -ItemType Directory -Force $InstallDir | Out-Null
New-Item -ItemType Directory -Force "$InstallDir\bin" | Out-Null
New-Item -ItemType Directory -Force "$InstallDir\configs" | Out-Null
New-Item -ItemType Directory -Force "$InstallDir\tools" | Out-Null

$scriptDir = Split-Path -Parent $PSScriptRoot
if (-not (Test-Path "$scriptDir\bin\core-agent.exe")) {
    Write-Host "ERROR: bin\core-agent.exe not found" -ForegroundColor Red
    exit 1
}

Copy-Item "$scriptDir\bin\core-agent.exe" "$InstallDir\bin\core-agent.exe" -Force
Copy-Item "$scriptDir\bin\tray.exe" "$InstallDir\bin\tray.exe" -Force
Copy-Item "$scriptDir\bin\agent-watchdog.exe" "$InstallDir\bin\agent-watchdog.exe" -Force
Copy-Item "$scriptDir\tools\verify.exe" "$InstallDir\tools\verify.exe" -Force

# Password hash (plain for now, tools generates)
Set-Content -Path "$InstallDir\configs\agent.password" -Value $Password -Encoding UTF8

$agentCfg = Get-Content "$scriptDir\configs\agent.json" | ConvertFrom-Json
$agentCfg.install_token = $InstallToken
$agentCfg.server_primary = $Server
$agentCfg.spool_dir = "$InstallDir\data\spool"
$agentCfg | ConvertTo-Json -Depth 10 | Set-Content "$InstallDir\configs\agent.json"

sc.exe stop ITAgentService 2>$null | Out-Null
Start-Sleep -Seconds 2
sc.exe delete ITAgentService 2>$null | Out-Null
Start-Sleep -Seconds 2
sc.exe create ITAgentService binPath= "`"$InstallDir\bin\core-agent.exe`" -config `"$InstallDir\configs\agent.json`"" start= auto | Out-Null
sc.exe failure ITAgentService reset= 60 actions= restart/5000/restart/10000/restart/30000 | Out-Null

$action = New-ScheduledTaskAction -Execute "$InstallDir\bin\tray.exe"
$trigger = New-ScheduledTaskTrigger -AtLogOn
Register-ScheduledTask -TaskName "ITAgentTray" -Action $action -Trigger $trigger -User $env:USERNAME -RunLevel Limited -Force | Out-Null

Start-Service ITAgentService

Write-Host "Install OK" -ForegroundColor Green
Write-Host "Service started" -ForegroundColor Green

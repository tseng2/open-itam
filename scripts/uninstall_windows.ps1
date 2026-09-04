# ITAgent 卸载脚本 (Windows)
# 用法: .\uninstall_windows.ps1
param ()

if (-NOT ([Security.Principal.WindowsPrincipal][Security.Principal.WindowsIdentity]::GetCurrent()).IsInRole([Security.Principal.WindowsBuiltInRole]"Administrator")) {
    Write-Host "需要管理员权限" -ForegroundColor Red
    exit 1
}

Write-Host "=== 卸载 IT Agent ===" -ForegroundColor Yellow

Stop-Service ITAgentService -ErrorAction SilentlyContinue
sc.exe delete ITAgentService
Unregister-ScheduledTask -TaskName "ITAgentTray" -Confirm:$false -ErrorAction SilentlyContinue

$InstallDir = "C:\ProgramData\ITAgent"
if (Test-Path $InstallDir) {
    Remove-Item -Recurse -Force $InstallDir
}

Write-Host "卸载完成" -ForegroundColor Green

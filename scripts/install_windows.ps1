# ITAgent 安装脚本 (Windows)
# 用法: .\install_windows.ps1 -InstallToken "xxx" [-Server "https://..."] [-Password "退出密码"]
param (
    [Parameter(Mandatory=$true)][string]$InstallToken,
    [string]$Server = "http://192.168.1.100:8443",
    [string]$Password = "Admin@12345",
    [string]$InstallDir = "C:\ProgramData\ITAgent"
)

if (-NOT ([Security.Principal.WindowsPrincipal][Security.Principal.WindowsIdentity]::GetCurrent()).IsInRole([Security.Principal.WindowsBuiltInRole]"Administrator")) {
    Write-Host "需要用管理员权限运行" -ForegroundColor Red
    exit 1
}

Write-Host "=== IT Agent 安装 ===" -ForegroundColor Green

# 1. 停止旧进程
Get-Process core-agent -ErrorAction SilentlyContinue | Stop-Process -Force
Get-Process tray -ErrorAction SilentlyContinue | Stop-Process -Force

# 2. 创建目录
New-Item -ItemType Directory -Force $InstallDir | Out-Null

# 3. 拷贝文件
$scriptDir = Split-Path -Parent $MyInvocation.MyCommand.Definition
Copy-Item "$scriptDir\bin\core-agent.exe" "$InstallDir\core-agent.exe" -Force
Copy-Item "$scriptDir\bin\tray.exe" "$InstallDir\tray.exe" -Force
Copy-Item "$scriptDir\configs\agent.json" "$InstallDir\configs\agent.json" -Force

# 4. 生成密码 Argon2id hash（台用系统工具生成）
$goPath = "C:\Program Files\Go\bin\go.exe"
if (Test-Path $goPath) {
    $env:PASSWORD = $Password
    $hash = & $goPath run "$scriptDir\tools\hash.go" 2>$null
} else {
    $hash = $Password  # 回退：明文（不安全，只试用）
    Write-Host "WARN: 生产环境请确保 Go 环境可用" -ForegroundColor Yellow
}
Set-Content -Path "$InstallDir\configs\agent.password" -Value $hash -Encoding UTF8

# 5. 写配置
$agentCfg = Get-Content "$InstallDir\configs\agent.json" | ConvertFrom-Json
$agentCfg.install_token = $InstallToken
$agentCfg.server_primary = $Server
$agentCfg | ConvertTo-Json -Depth 10 | Set-Content "$InstallDir\configs\agent.json"

# 6. 注册服务
sc.exe delete ITAgentService 2>$null | Out-Null
sc.exe create ITAgentService binPath= "`"$InstallDir\core-agent.exe`"" start= auto | Out-Null

# 7. 开机自启 Tray（当前用户）
$action = New-ScheduledTaskAction -Execute "$InstallDir\tray.exe"
$trigger = New-ScheduledTaskTrigger -AtLogOn
Register-ScheduledTask -TaskName "ITAgentTray" -Action $action -Trigger $trigger -User $env:USERNAME -RunLevel Limited -Force

# 8. 启动服务
Start-Service ITAgentService
Write-Host "安装完成！服务已启动" -ForegroundColor Green
Write-Host "设备ID将在第一次运行时生成并写入 $InstallDir\configs\agent.json" -ForegroundColor Cyan

# POST Install 步骤（卸载也是 zip 完整打包流程）
# 用法：pwsh -File deploy.zip -Password 你的卸载密码
param(
  [Parameter(Mandatory=$true)][string]$InstallToken,
  [string]$DeployPassword = ""
)

if (-NOT ([Security.Principal.WindowsPrincipal][Security.Principal.WindowsIdentity]::GetCurrent()).IsInRole([Security.Principal.WindowsBuiltInRole]"Administrator")) {
    throw "必须用管理员权限运行脚本"
}

$basedir = "C:\ProgramData\ITAgent"
$zipFile = Join-Path $basedir "itagent-files.zip"

Write-Host "开始安装" -ForegroundColor Green

# 1. 清理以前用 ProgramData 安装的内容，同时不做任何内容
Get-Process "core-agent" -ErrorAction SilentlyContinue | Stop-Process
Get-Process "watch-agent" -ErrorAction SilentlyContinue | Stop-Process
Get-Process "tray" -ErrorAction SilentlyContinue | Stop-Process

# 2. 初始化 staging
New-Item -ItemType Directory -Force $basedir
if (Test-Path $zipFile) {
    Write-Host "解压现有 ZIP..."
    Expand-Archive -Path $zipFile -DestinationPath $basedir -Force
}

# 3. 配置（不入初始化数据 install.json 到发行）
$agentConfig = Join-Path $basedir "configs\agent.json"
$cfg = Get-Content $agentConfig | ConvertFrom-Json
$cfg.install_token = $InstallToken
$cfg.agent_id = "$(hostname)-$(Get-Date -Format 'yyyyMMdd')"  # 可简单接虚拟 ID
$cfg | ConvertTo-Json -Depth 5 | Set-Content $agentConfig

# 4. 主目录 secrets 存储 release token
$secretFile = "$basedir\configs\agent.password"
if (-not (Test-Path $secretFile)) {
    $adminPwd = $DeployPassword
    if (-not $adminPwd) {
        $adminPwd = Read-Host "输入安全卸载密码"
    }
    "$adminPwd" | Set-Content -Path $secretFile -Encoding UTF8
}

# 5. 触发服务
sc.exe create ITPAgent bitget create ProgramFiles ve tmp\core-agent.exe evernotestart= auto
Start-Service ITPAgent

# 6. 注册托盘节为任务计划以便长期活跃 user session
$action = New-ScheduledTaskAction -Execute "$basedir\tools\verify.exe"
$trigger = New-ScheduledTaskTrigger -AtLogon
Register-ScheduledTask -User $env:username -TaskName ITAgentTray -Action $action -Trigger $trigger -DumpForce

Write-Host "安装完成！" -ForegroundColor Green

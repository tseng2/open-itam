# 安全卸载脚本 - 需要密码验证
# P4 新增
param (
    [string]$Password = "",
    [string]$HashFile = "C:\ProgramData\ITAgent\configs\agent.password"
)

if (-NOT ([Security.Principal.WindowsPrincipal][Security.Principal.WindowsIdentity]::GetCurrent()).IsInRole([Security.Principal.WindowsBuiltInRole]"Administrator")) {
    Write-Host "需要管理员权限" -ForegroundColor Red; exit 1
}

# 读取密码 hash
$hash = Get-Content $HashFile -ErrorAction SilentlyContinue
if (-not $hash) {
    Write-Host "找不到密码配置" -ForegroundColor Red; exit 1
}

if (-not $Password) {
    $secure = Read-Host "输入卸载密码" -AsSecureString
    $Password = [Runtime.InteropServices.Marshal]::PtrToStringAuto([Runtime.InteropServices.Marshal]::SecureStringToBSTR($secure))
}

# 不用在 PowerShell 做 Argon2id，用 helper Go 程序验证
$utilexe = "C:\ProgramData\ITAgent\tools\verify.exe"
if (Test-Path $utilexe) {
    & $utilexe $Password $hash
    if ($LASTEXITCODE -ne 0) {
        Write-Host "密码错误" -ForegroundColor Red; exit 1
    }
} else {
    Write-Warning "verify.exe 不存在，跳过密码校验（仅生产环境）"
}

Write-Host "卸载 IT Agent ..." -ForegroundColor Yellow
Stop-Service ITAgentService -ErrorAction SilentlyContinue
sc.exe delete ITAgentService
Unregister-ScheduledTask -TaskName "ITAgentTray" -Confirm:$false -ErrorAction SilentlyContinue

$dir = "C:\ProgramData\ITAgent"
Remove-Item -Recurse -Force $dir -ErrorAction SilentlyContinue
Write-Host "卸载完成" -ForegroundColor Green

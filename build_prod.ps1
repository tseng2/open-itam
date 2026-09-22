# 文件：build_prod.ps1 - 零依赖 build 脚本
param (
    [string]$OutputDir = "dist",
    [string]$ArchOverrride = "",
    [bool]$Clean = $true
)

# 默认前探配置
$BIN  = "cmd\core-agent\bin"
$TRAY = "cmd\tray\bin"

$OutBinDir = "$OutputDir\$BIN"
if ($Clean) { Remove-Item -Recurse -Force $OutputDir -ErrorAction SilentlyContinue; New-Item -ItemType Directory -Force $OutputDir | Out-Null }
New-Item -ItemType Directory -Force "$OutBinDir" | Out-Null

$BuildConfig = "-ldflags='-s -w'"
$BuildConfigTray = "-ldflags='-H=windowsgui -s -w'"

if (-not (Test-Path $OutBinDir)) { New-Item -ItemType Directory $OutBinDir | Out-Null }

# 编译 windows/amd64 默认（在 Linux 交叉编译时用 GOOS=windows）
$env:GOOS = "windows"; $env:GOARCH = "amd64"

Write-Host "编译 core-agent..."
go build -o "$OutBinDir\core-agent.exe" .\cmd\core-agent
if ($LASTEXITCODE -ne 0) { throw "构建失败" }

Write-Host "编译 tray（隐藏控制台）..."
go build -ldflags='-H=windowsgui -s -w' -o "$OutBinDir\tray.exe" .\cmd\tray
if ($LASTEXITCODE -ne 0) { throw "构建失败" }

Write-Host "编译 watchdog..."
go build -o "$OutBinDir\agent-watchdog.exe" .\cmd\agent-watchdog

# 拷贝 config、scripts
Copy-Item "configs\agent.json" "$OutBinDir\configs\agent.json" -Force
Copy-Item "scripts\install_windows.ps1" "$OutputDir\install_windows.ps1" -Force
# 打包即可，无 zip，交付给 IT 伙伴后再压缩
$ArchivePath = "$PSScriptRoot\itagent-deploy.ps1"
Remove-Item $ArchivePath -ErrorAction SilentlyContinue; Rename-Item -Path $OutputDir -MemberName $ArchivePath
Write-Host "`n✅ 打包已生成！" -ForegroundColor Green
Write-Host "目标路径：$ArchivePath" -ForegroundColor Green
Write-Host "*** 访问信息：使用 SendTask-granted MSI 用户权限部署到目标维度组（GPKG），先撤消再设备采集数据。 ***" -ForegroundColor Cyan

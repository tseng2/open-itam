$ErrorActionPreference = "Continue"
Stop-Process -Name tray -Force -ErrorAction SilentlyContinue
Stop-Service ITAgentService -Force -ErrorAction SilentlyContinue
Start-Sleep 2
Copy-Item 'E:\ai_work\trae\itagent\itagent-deploy\bin\core-agent.exe' 'C:\ProgramData\ITAgent\bin\core-agent.exe' -Force
Copy-Item 'E:\ai_work\trae\itagent\itagent-deploy\bin\tray.exe' 'C:\ProgramData\ITAgent\bin\tray.exe' -Force
Copy-Item 'E:\ai_work\trae\itagent\itagent-deploy\tools\verify.exe' 'C:\ProgramData\ITAgent\tools\verify.exe' -Force
$h = & 'C:\ProgramData\ITAgent\tools\verify.exe' hash 'Admin@12345'
[IO.File]::WriteAllText('C:\ProgramData\ITAgent\configs\agent.password', ([string]$h).Trim())
Start-Service ITAgentService
Start-Process -FilePath 'C:\ProgramData\ITAgent\bin\tray.exe' -WorkingDirectory 'C:\ProgramData\ITAgent\bin'
"updated" | Set-Content 'E:\ai_work\trae\itagent\itagent-deploy\update.log'
Get-Content 'C:\ProgramData\ITAgent\configs\agent.password' | Set-Content 'E:\ai_work\trae\itagent\itagent-deploy\pwdcheck.log'

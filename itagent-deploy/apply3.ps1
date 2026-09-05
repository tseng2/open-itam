Stop-Process -Name tray -Force -ErrorAction SilentlyContinue
Stop-Service ITAgentService -Force -ErrorAction SilentlyContinue
Start-Sleep 2
Copy-Item 'E:\ai_work\trae\itagent\itagent-deploy\bin\core-agent.exe' 'C:\ProgramData\ITAgent\bin\core-agent.exe' -Force
Copy-Item 'E:\ai_work\trae\itagent\itagent-deploy\bin\tray.exe' 'C:\ProgramData\ITAgent\bin\tray.exe' -Force
Copy-Item 'E:\ai_work\trae\itagent\itagent-deploy\tools\smartctl.exe' 'C:\ProgramData\ITAgent\tools\smartctl.exe' -Force
Start-Service ITAgentService
"done $(Get-Date)" | Out-File 'E:\ai_work\trae\itagent\itagent-deploy\apply3.log'

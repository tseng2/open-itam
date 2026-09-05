Stop-Process -Name tray -Force -ErrorAction SilentlyContinue
Start-Sleep 1
Copy-Item 'E:\ai_work\trae\itagent\itagent-deploy\bin\tray.exe' 'C:\ProgramData\ITAgent\bin\tray.exe' -Force
"tray updated $(Get-Date)" | Set-Content 'E:\ai_work\trae\itagent\itagent-deploy\tray_update.log'

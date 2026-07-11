@echo off
setlocal
powershell.exe -NoProfile -ExecutionPolicy Bypass -File "%~dp0test.ps1" %*
exit /b %errorlevel%

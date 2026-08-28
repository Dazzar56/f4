@echo off
setlocal

set "LOG=%CD%\terminal-width-windows-%COMPUTERNAME%-%RANDOM%.log"
echo Running Windows terminal-width probe.
echo The child commands inherit the current console/ConPTY size.
echo Log: "%LOG%"

powershell.exe -NoLogo -NoProfile -ExecutionPolicy Bypass -File "%~dp0windows-width-probe.ps1" -LogPath "%LOG%"
set "RC=%ERRORLEVEL%"

echo.
echo Probe exit code: %RC%
echo Log: "%LOG%"
exit /b %RC%

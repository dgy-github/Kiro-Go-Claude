@echo off
setlocal
cd /d "%~dp0"

if not exist "kiro-go.exe" (
  echo [Kiro-Go] kiro-go.exe not found in %CD%
  pause
  exit /b 1
)

if not exist "data\logs" mkdir "data\logs"

for /f "tokens=1-4 delims=/ " %%a in ("%date%") do set TODAY=%%a-%%b-%%c
for /f "tokens=1-3 delims=:." %%a in ("%time%") do set NOW=%%a%%b%%c
set NOW=%NOW: =0%

echo [Kiro-Go] Starting Claude gateway on http://127.0.0.1:8080
echo [Kiro-Go] Logs: data\logs\kiro-go-claude-%TODAY%-%NOW%.stdout.log

start "Kiro-Go Claude Gateway" /min "%CD%\kiro-go.exe" 1>>"data\logs\kiro-go-claude-%TODAY%-%NOW%.stdout.log" 2>>"data\logs\kiro-go-claude-%TODAY%-%NOW%.stderr.log"

timeout /t 2 >nul
powershell -NoProfile -ExecutionPolicy Bypass -Command "try { (Invoke-WebRequest -UseBasicParsing -Uri 'http://127.0.0.1:8080/health' -TimeoutSec 5).Content } catch { 'Kiro-Go started, but health check failed: ' + $_.Exception.Message }"
endlocal

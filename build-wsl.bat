@echo off
REM Build script for InfluxDB binaries using Docker in WSL
REM This is a Windows batch wrapper for build-wsl.sh

echo ======================================
echo InfluxDB Build Script (WSL + Docker)
echo ======================================
echo.

REM Check if WSL is available
wsl --version >nul 2>&1
if errorlevel 1 (
    echo Error: WSL is not installed or not available
    echo Please install WSL2 from Microsoft Store
    exit /b 1
)

REM Change to script directory
cd /d "%~dp0"

echo Starting build using WSL...
echo.

REM Run the bash script in WSL
wsl bash -c "cd /mnt/c/code/influxdb && ./build-wsl.sh"

if errorlevel 1 (
    echo.
    echo Build failed!
    exit /b 1
)

echo.
echo ======================================
echo Build completed successfully!
echo ======================================
echo.
echo Binaries are in: build-output\
echo.
pause


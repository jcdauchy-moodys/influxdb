@echo off
REM Simple InfluxDB CLI with JWT - More robust URL handling
REM Usage: start_influx_simple.cmd [jwt_token] [host] [port] [database] [ssl]

setlocal

REM Default values
set DEFAULT_HOST=localhost
set DEFAULT_PORT=8086
set INFLUX_EXE=influx.exe

REM Check if influx executable exists
if not exist "%INFLUX_EXE%" (
    echo Error: %INFLUX_EXE% not found in current directory
    echo Please build the influx client first: go build -o influx.exe ./cmd/influx
    pause
    exit /b 1
)

REM Get parameters
set JWT_TOKEN=eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiJpbmZsdXhkYiIsImNsdXN0ZXIiOiJjbHVzdGVyLW1vbml0b3JpbmctbnByZDIiLCJleHAiOjE3OTc1OTI2NjMsImtpZCI6Im1vbml0b3Jpbmctc3RhY2siLCJzY29wZSI6e319.VipJ8rvMq6xPkkJbsDMsvNRr83yxAeXwZtKeCSikccxDKb1hgdy-7uxZBQv7H8Ea8xc/HF0xng31Y3DVs7MZ7hoS8VeMAq50e0b/Wc559+YztazjNLG5OWieFzmgLqdCO8t9j5Rviote8IZgfYefeF4Q7l+e0+LH+ELsRYEq5+ENl07r+YPKISOURiXw6AYBN+K73csszqTwHRR1WnoRJuI71pvkwYZNXH1wF9ZiWHOObAn0tMXlmPq82NmVKdyAKFIpCpUpMkJ5s31Nf0JlzwMChuOcGzSyC0SevrJpa7U2zgfXLWwrqYrBu1ejgBdwgHckYdyw0s4fPk8l7TW8tA
set HOST=monitoring-cluster-brt-nprd.bankingcloud.moodysanalytics.net
set PORT=443
set DATABASE=azkaban_db
set USE_SSL=y
set INFLUXDB_SUBPATH=/influxdb

REM Remove quotes and set defaults
if "%JWT_TOKEN%"=="" (
    echo.
    echo ===============================================
    echo   InfluxDB CLI with JWT Authentication
    echo ===============================================
    echo.
    set /p JWT_TOKEN="Enter JWT Token: "
)

if "%JWT_TOKEN%"=="" (
    echo Error: JWT token is required
    pause
    exit /b 1
)

if "%HOST%"=="" set HOST=%DEFAULT_HOST%
if "%PORT%"=="" set PORT=%DEFAULT_PORT%

if "%DATABASE%"=="" (
    set /p DATABASE="Enter Database Name (optional): "
)

if "%USE_SSL%"=="" (
    set /p USE_SSL="Use SSL? (y/n, default=n): "
)

REM Build the command
set INFLUX_CMD=%INFLUX_EXE% -jwt "%JWT_TOKEN%" -path-prefix "%INFLUXDB_SUBPATH%" -host "%HOST%" -port %PORT%

if not "%DATABASE%"=="" (
    set INFLUX_CMD=%INFLUX_CMD% -database "%DATABASE%"
)

if /i "%USE_SSL%"=="y" (
    set INFLUX_CMD=%INFLUX_CMD% -ssl
    set SSL_STATUS=Enabled
) else (
    set SSL_STATUS=Disabled
)

REM Display connection info
echo.
echo ===============================================
echo   Starting InfluxDB CLI Session
echo ===============================================
echo Host: %HOST%
echo Port: %PORT%
if not "%DATABASE%"=="" echo Database: %DATABASE%
echo SSL: %SSL_STATUS%
echo JWT: %JWT_TOKEN:~0,20%...
echo.
echo Command: %INFLUX_CMD%
echo.

REM Set environment variable as backup
set INFLUX_JWT=%JWT_TOKEN%

REM Start the interactive session
echo Starting interactive session...
echo.
%INFLUX_CMD%

REM Check exit code
if errorlevel 1 (
    echo.
    echo ===============================================
    echo   Session ended with error
    echo ===============================================
    pause
) else (
    echo.
    echo ===============================================
    echo   Session ended successfully
    echo ===============================================
)

endlocal


# InfluxDB CLI with JWT Authentication Scripts

This directory contains scripts to easily start InfluxDB CLI sessions with JWT authentication.

## Prerequisites

1. **Build the InfluxDB CLI with JWT support:**
   ```bash
   go build -o influx.exe ./cmd/influx
   ```

2. **Have a valid JWT token** for your InfluxDB instance

## Available Scripts

### 1. `start_influx_jwt.cmd` (Full-featured Windows batch script)

**Interactive usage:**
```cmd
start_influx_jwt.cmd
```
This will prompt you for:
- JWT token
- Database name (optional)

**Command line usage:**
```cmd
start_influx_jwt.cmd "your.jwt.token" "http://localhost:8086" "mydatabase"
start_influx_jwt.cmd "your.jwt.token" "https://influx.company.com:8086" "production"
```

**Features:**
- Interactive prompts for missing parameters
- URL parsing (supports both HTTP and HTTPS)
- SSL detection
- Error handling
- Connection info display

### 2. `influx_jwt.cmd` (Simple Windows batch script)

**Usage:**
```cmd
influx_jwt.cmd "your.jwt.token" mydatabase
influx_jwt.cmd "your.jwt.token"
```

**Using environment variable:**
```cmd
set INFLUX_JWT=your.jwt.token
influx_jwt.cmd "" mydatabase
```

### 3. `Start-InfluxJWT.ps1` (PowerShell script)

**Usage:**
```powershell
.\Start-InfluxJWT.ps1 -JwtToken "your.jwt.token" -Database "metrics"
.\Start-InfluxJWT.ps1 -JwtToken "your.jwt.token" -Url "https://influx.company.com:8086" -Database "production"
```

**Using environment variable:**
```powershell
$env:INFLUX_JWT = "your.jwt.token"
.\Start-InfluxJWT.ps1 -JwtToken $env:INFLUX_JWT -Database "metrics"
```

**Get help:**
```powershell
Get-Help .\Start-InfluxJWT.ps1 -Full
```

**Features:**
- Parameter validation
- Comprehensive help documentation
- URL parsing
- Colored output
- Error handling

## Environment Variables

All scripts support the `INFLUX_JWT` environment variable:

```cmd
set INFLUX_JWT=your.jwt.token
start_influx_jwt.cmd
```

```powershell
$env:INFLUX_JWT = "your.jwt.token"
.\Start-InfluxJWT.ps1 -JwtToken $env:INFLUX_JWT
```

## Examples

### Basic Connection
```cmd
# Using local InfluxDB with JWT
influx_jwt.cmd "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9..." mydb
```

### Secure Connection
```cmd
# Using HTTPS InfluxDB server
start_influx_jwt.cmd "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9..." "https://influx.company.com:8086" "production"
```

### Environment Variable Usage
```cmd
# Set once, use multiple times
set INFLUX_JWT=eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...
influx_jwt.cmd "" metrics
influx_jwt.cmd "" logs
influx_jwt.cmd "" monitoring
```

## Troubleshooting

### "influx.exe not found"
Make sure you've built the InfluxDB CLI:
```bash
go build -o influx.exe ./cmd/influx
```

### "JWT token is required"
Provide a valid JWT token either as a parameter or environment variable.

### Connection errors
- Check that the InfluxDB server is running
- Verify the URL and port
- Ensure your JWT token is valid and not expired
- For HTTPS connections, make sure SSL certificates are valid

## JWT Token Format

JWT tokens typically look like:
```
eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIiwibmFtZSI6IkpvaG4gRG9lIiwiaWF0IjoxNTE2MjM5MDIyfQ.SflKxwRJSMeKKF2QT4fwpMeJf36POk6yJV_adQssw5c
```

The token should be passed as a single string without line breaks.

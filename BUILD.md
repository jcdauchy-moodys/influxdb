# Building InfluxDB with Parquet Export Support

This document describes how to build InfluxDB binaries with the new Parquet export functionality in `influx_inspect`.

## Overview

The build includes:
- **influxd**: InfluxDB server daemon
- **influx**: InfluxDB CLI client
- **influx_inspect**: Inspection tool with parquet export support
- **influx_tools**: Additional tools including parquet export

## Prerequisites

### Windows with WSL2

1. **WSL2** installed with Ubuntu or Debian distribution
2. **Docker Desktop for Windows** with WSL2 integration enabled
3. Access to the influxdb source code

## Building with Docker (Recommended)

### Option 1: Using WSL (Windows)

From Windows Command Prompt or PowerShell:

```cmd
cd c:\code\influxdb
build-wsl.bat
```

From WSL:

```bash
cd /mnt/c/code/influxdb
./build-wsl.sh
```

### Option 2: Using Docker directly

```bash
# Build the Docker image
docker build \
  --build-arg GOLANG_IMAGE=golang:1.22 \
  --build-arg ARTIFACT_NAME=influxdb-binaries.tgz \
  --build-arg INFLUXDB_VERSION=dev \
  -f Dockerfile-build-local \
  -t influxdb-builder:latest \
  .

# Extract the binaries
docker create --name temp-influxdb influxdb-builder:latest
docker cp temp-influxdb:/output/. ./build-output/
docker rm temp-influxdb
```

## Build Output

After a successful build, binaries will be in `./build-output/`:

- `influxd` - InfluxDB server
- `influx` - InfluxDB CLI client  
- `influx_inspect` - Inspection tool with **parquet export support**
- `influx_tools` - Additional tools
- `influxdb-binaries.tgz` - Archived binaries

## Using the Parquet Export

Once built, you can export InfluxDB data to Parquet format:

```bash
# Make executable (Linux/WSL)
chmod +x ./build-output/influx_inspect

# Export a database to Parquet
./build-output/influx_inspect export-parquet \
  --database mydb \
  --output ./parquet-export

# Preview the schema without exporting
./build-output/influx_inspect export-parquet \
  --database mydb \
  --dry-run

# Export with type resolution
./build-output/influx_inspect export-parquet \
  --database mydb \
  --resolve-types "cpu.usage=float,memory.free=int" \
  --output ./parquet-export
```

## Build Configuration

### Makefile-build

The `Makefile-build` is used during the Docker build process. Key changes:

- `influx_inspect` now uses `GO_BUILD_TOOLS` instead of `GO_BUILD` to support parquet dependencies
- This adds the necessary protobuf conflict resolution: `-X google.golang.org/protobuf/reflect/protoregistry.conflictPolicy=ignore`

### Dockerfile-build-local

- Copies the local codebase instead of cloning from git
- Installs all necessary build dependencies (Go, Rust, protobuf, etc.)
- Builds all binaries in a single container
- Exports binaries and creates an archive

## Troubleshooting

### Docker Build Fails

1. Ensure Docker Desktop is running
2. Check WSL2 integration is enabled in Docker Desktop settings
3. Verify you have enough disk space (build requires ~2GB)

### Build Takes Too Long

The first build downloads dependencies and may take 10-15 minutes. Subsequent builds are faster due to Docker layer caching.

### Binaries Not Found

Ensure the Docker build completed successfully. Check the output directory:

```bash
ls -la ./build-output/
```

### Permission Denied (WSL)

Make the binaries executable:

```bash
chmod +x ./build-output/influx_inspect
```

## Development Build (Local)

For development without Docker:

```bash
# Set environment
export PKG_CONFIG=./pkg-config.sh
export GOOS=linux
export GOARCH=amd64

# Build influx_inspect with parquet support
make -f Makefile-build influx_inspect

# Binary will be in: bin/linux/influx_inspect
```

Note: Local builds require:
- Go 1.22 or later
- Rust toolchain
- protobuf compiler
- All dependencies installed

## Additional Information

For more details on using the parquet export feature, see:
- [cmd/influx_inspect/exportparquet/README.md](cmd/influx_inspect/exportparquet/README.md)
- [cmd/influx_inspect/README.md](cmd/influx_inspect/README.md)

## Build Scripts Reference

- `build-wsl.sh` - Main build script for WSL/Linux
- `build-wsl.bat` - Windows wrapper for WSL build
- `Dockerfile-build-local` - Docker build definition using local source
- `Makefile-build` - Makefile with parquet support configuration


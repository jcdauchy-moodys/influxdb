#!/bin/bash
# Build script for InfluxDB binaries using Docker in WSL
# This script builds influxd, influx, influx_inspect (with parquet), and influx_tools
# Usage: ./build-wsl.sh [tools-only]

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

echo -e "${GREEN}======================================${NC}"
echo -e "${GREEN}InfluxDB Build Script (WSL + Docker)${NC}"
echo -e "${GREEN}======================================${NC}"
echo ""

# Check for build mode
BUILD_MODE="${1:-full}"
if [ "$BUILD_MODE" = "tools-only" ]; then
    BUILD_INFLUXD="false"
    echo -e "${YELLOW}Build Mode: Tools Only (influx, influx_inspect, influx_tools)${NC}"
else
    BUILD_INFLUXD="true"
    echo -e "${YELLOW}Build Mode: Full Build (influxd + tools)${NC}"
fi
echo ""

# Configuration
DOCKER_IMAGE="${DOCKER_IMAGE:-golang:1.23}"
ARTIFACT_NAME="${ARTIFACT_NAME:-influxdb-binaries.tgz}"
INFLUXDB_VERSION="${INFLUXDB_VERSION:-dev-$(date +%Y%m%d-%H%M%S)}"
OUTPUT_DIR="./build-output"

# Check if Docker is available
if ! command -v docker &> /dev/null; then
    echo -e "${RED}Error: Docker is not installed or not in PATH${NC}"
    echo "Please install Docker Desktop for Windows and ensure WSL integration is enabled"
    exit 1
fi

# Check if Docker daemon is running
if ! docker info &> /dev/null; then
    echo -e "${RED}Error: Docker daemon is not running${NC}"
    echo "Please start Docker Desktop"
    exit 1
fi

echo -e "${YELLOW}Build Configuration:${NC}"
echo "  Docker Image: $DOCKER_IMAGE"
echo "  Build influxd: $BUILD_INFLUXD"
echo "  Artifact Name: $ARTIFACT_NAME"
echo "  Version: $INFLUXDB_VERSION"
echo "  Output Directory: $OUTPUT_DIR"
echo ""

# Create output directory
mkdir -p "$OUTPUT_DIR"

# Check if Makefile-build exists
if [ ! -f "Makefile-build" ]; then
    echo -e "${RED}Error: Makefile-build not found${NC}"
    echo "Please ensure you're running this script from the influxdb repository root"
    exit 1
fi

# Check if Dockerfile-build-local exists
if [ ! -f "Dockerfile-build-local" ]; then
    echo -e "${RED}Error: Dockerfile-build-local not found${NC}"
    echo "Creating Dockerfile-build-local from template..."
    echo "Please ensure Dockerfile-build-local exists in the repository root"
    exit 1
fi

echo -e "${GREEN}Starting Docker build...${NC}"
echo ""

# Build the Docker image and extract artifacts
docker build \
    --build-arg GOLANG_IMAGE="$DOCKER_IMAGE" \
    --build-arg ARTIFACT_NAME="$ARTIFACT_NAME" \
    --build-arg INFLUXDB_VERSION="$INFLUXDB_VERSION" \
    --build-arg BUILD_INFLUXD="$BUILD_INFLUXD" \
    -f Dockerfile-build-local \
    -t influxdb-builder:latest \
    . || {
        echo -e "${RED}Docker build failed!${NC}"
        exit 1
    }

echo ""
echo -e "${GREEN}Build completed successfully!${NC}"
echo -e "${GREEN}Extracting binaries...${NC}"
echo ""

# Create a temporary container to copy files from
CONTAINER_ID=$(docker create influxdb-builder:latest)

# Copy the output files
docker cp "$CONTAINER_ID:/output/." "$OUTPUT_DIR/" || {
    echo -e "${RED}Failed to copy binaries from container${NC}"
    docker rm "$CONTAINER_ID" > /dev/null
    exit 1
}

# Clean up the temporary container
docker rm "$CONTAINER_ID" > /dev/null

# Extract the tar file if it exists
if [ -f "$OUTPUT_DIR/$ARTIFACT_NAME" ]; then
    echo ""
    echo -e "${GREEN}Extracting archive...${NC}"
    mkdir -p "$OUTPUT_DIR/extracted"
    tar -xzf "$OUTPUT_DIR/$ARTIFACT_NAME" -C "$OUTPUT_DIR/extracted" || {
        echo -e "${YELLOW}Warning: Failed to extract archive${NC}"
    }
    if [ -d "$OUTPUT_DIR/extracted" ] && [ "$(ls -A $OUTPUT_DIR/extracted)" ]; then
        echo -e "${GREEN}Archive extracted to: $OUTPUT_DIR/extracted/${NC}"
    fi
fi

echo -e "${GREEN}======================================${NC}"
echo -e "${GREEN}Build Complete!${NC}"
echo -e "${GREEN}======================================${NC}"
echo ""
echo "Binaries extracted to: $OUTPUT_DIR"
echo ""
echo "Contents of $OUTPUT_DIR:"
ls -lh "$OUTPUT_DIR"
echo ""

if [ -d "$OUTPUT_DIR/extracted" ]; then
    echo "Contents of $OUTPUT_DIR/extracted/:"
    ls -lh "$OUTPUT_DIR/extracted"
    echo ""
fi

# Check for specific binaries
if [ -f "$OUTPUT_DIR/influx_inspect" ]; then
    echo -e "${GREEN}✓ influx_inspect${NC} - Ready with parquet export support!"
    echo "  Usage: ./build-output/influx_inspect export-parquet --help"
else
    echo -e "${YELLOW}⚠ influx_inspect not found in output${NC}"
fi

if [ -f "$OUTPUT_DIR/influx_tools" ]; then
    echo -e "${GREEN}✓ influx_tools${NC} - Ready!"
else
    echo -e "${YELLOW}⚠ influx_tools not found in output${NC}"
fi

if [ -f "$OUTPUT_DIR/influxd" ]; then
    echo -e "${GREEN}✓ influxd${NC} - Ready!"
else
    echo -e "${YELLOW}⚠ influxd not found in output${NC}"
fi

if [ -f "$OUTPUT_DIR/influx" ]; then
    echo -e "${GREEN}✓ influx${NC} - Ready!"
else
    echo -e "${YELLOW}⚠ influx not found in output${NC}"
fi

if [ -f "$OUTPUT_DIR/$ARTIFACT_NAME" ]; then
    echo ""
    echo -e "${GREEN}✓ Archive created:${NC} $OUTPUT_DIR/$ARTIFACT_NAME"
fi

echo " **** Copy to S3 "
echo "aws s3 cp build-output/influxdb-binaries.tgz s3://ma-ers-brt-atlas-jcd-perfs/influxdb/binaries/influxdb-binaries.tgz"
echo ""

echo ""
echo -e "${GREEN}To test the parquet export:${NC}"
echo "  1. Make the binary executable: chmod +x $OUTPUT_DIR/influx_inspect"
echo "  2. Run: $OUTPUT_DIR/influx_inspect export-parquet --database=<db> --output=./parquet-export"
echo ""
echo -e "${YELLOW}Build options:${NC}"
echo "  Full build:       ./build-wsl.sh"
echo "  Tools only:       ./build-wsl.sh tools-only"
echo ""


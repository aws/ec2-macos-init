#!/bin/bash
set -e
set -o pipefail

echo "Building EC2 macOS Init..."

# Get commit date and version tag
COMMITDATE=$(git show -s --format=%ci HEAD)
VERSION=$(git describe --always --tags)
echo -e "Commit date: ${COMMITDATE}"
echo -e "Version: ${VERSION}"

# Build for darwin/amd64
echo "Running go build..."
GOOS=darwin GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X 'main.CommitDate=${COMMITDATE}' -X 'main.Version=${VERSION}'"

echo "Build complete"
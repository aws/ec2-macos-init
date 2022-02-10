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

for arch in amd64 arm64; do
    GOOS=darwin GOARCH="$arch" CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X 'main.CommitDate=${COMMITDATE}' -X 'main.Version=${VERSION}'" -o "ec2-macos-init_$arch"
done
lipo -create -output ec2-macos-init ec2-macos-init_amd64 ec2-macos-init_arm64

echo "Build complete"
#!/usr/bin/env bash
set -euo pipefail

log() { echo "> $*" >&2; }

if ! hash lipo 2>/dev/null; then
    log "unable to build universal binary without 'lipo' (macOS only)"
    exit 1
fi

log "building EC2 macOS Init..."

# Get commit date and version tag
COMMITDATE="$(git show -s --format=%ci HEAD)"
VERSION="$(git describe --always --tags)"

log "Commit date: ${COMMITDATE}"
log "Version: ${VERSION}"

for arch in amd64 arm64; do
    log "building for darwin/$arch"
    GOOS=darwin GOARCH="$arch" CGO_ENABLED=0 \
        go build -trimpath \
        -ldflags="-s -w -X 'main.CommitDate=${COMMITDATE}' -X 'main.Version=${VERSION}'" \
        -o "ec2-macos-init_$arch"
done

lipo -create -output ec2-macos-init \
    ec2-macos-init_amd64 ec2-macos-init_arm64

log "Build complete"

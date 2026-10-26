#!/bin/sh
# Cross-compiles NowPlaying.exe for Windows from Linux/macOS (needs Go 1.22+).
cd "$(dirname "$0")" && GOOS=windows GOARCH=amd64 go build -ldflags "-s -w -H windowsgui" -o NowPlaying.exe . && echo "Built NowPlaying.exe"

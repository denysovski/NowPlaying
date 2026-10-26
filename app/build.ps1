# Builds NowPlaying.exe (needs Go 1.22+ from https://go.dev/dl/)
$ErrorActionPreference = "Stop"
Set-Location $PSScriptRoot
go build -ldflags "-s -w -H windowsgui" -o NowPlaying.exe .
Write-Host "Built $PSScriptRoot\NowPlaying.exe"

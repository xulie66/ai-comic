param([string]$TargetOS = 'windows', [string]$TargetArch = 'amd64', [string]$Output = '')
$ErrorActionPreference = 'Stop'
$root = Split-Path $PSScriptRoot -Parent
if (-not $Output) { $Output = Join-Path $root "bin/$TargetOS-$TargetArch/comic-server$(if ($TargetOS -eq 'windows') { '.exe' })" }
$Output = [IO.Path]::GetFullPath($Output)
New-Item -ItemType Directory -Force (Split-Path $Output -Parent) | Out-Null
$oldOS = $env:GOOS; $oldArch = $env:GOARCH; $oldCGO = $env:CGO_ENABLED
Push-Location $root
try {
    $env:GOOS = $TargetOS; $env:GOARCH = $TargetArch; $env:CGO_ENABLED = '0'
    & go build -trimpath -ldflags '-s -w' -o $Output ./cmd/server
    if ($LASTEXITCODE -ne 0) { throw 'Go build failed' }
    Write-Output "Built $Output"
} finally {
    Pop-Location
    $env:GOOS = $oldOS; $env:GOARCH = $oldArch; $env:CGO_ENABLED = $oldCGO
}

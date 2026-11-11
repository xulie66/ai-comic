param([string]$EnvFile = '.env', [string]$Binary = 'bin/windows-amd64/comic-server.exe', [switch]$BootstrapAdmin)
$ErrorActionPreference = 'Stop'
$root = Split-Path $PSScriptRoot -Parent
Push-Location $root
try {
    if (Test-Path -LiteralPath $EnvFile) {
        foreach ($line in Get-Content -LiteralPath $EnvFile -Encoding UTF8) {
            $entry = $line.Trim()
            if (-not $entry -or $entry.StartsWith('#')) { continue }
            if ($entry -notmatch '^([A-Za-z_][A-Za-z0-9_]*)=(.*)$') { throw 'Use literal KEY=value lines in env file' }
            $key = $Matches[1]; $value = $Matches[2]
            if ($value.Length -ge 2 -and (($value.StartsWith('"') -and $value.EndsWith('"')) -or ($value.StartsWith("'") -and $value.EndsWith("'")))) { $value = $value.Substring(1, $value.Length - 2) }
            [Environment]::SetEnvironmentVariable($key, $value, 'Process')
        }
    }
    $exe = (Resolve-Path -LiteralPath $Binary).Path
    if ($BootstrapAdmin) {
        if (-not $env:GO_SERVICE_ADMIN_USERNAME -or -not $env:GO_SERVICE_ADMIN_PASSWORD) { throw 'Set explicit bootstrap admin username and password first' }
        & $exe -bootstrap-admin
    } else { & $exe }
    if ($LASTEXITCODE -ne 0) { throw 'Service exited with an error; inspect the private data/logs/server.jsonl' }
} finally { Pop-Location }

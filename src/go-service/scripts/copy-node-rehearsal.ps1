param([Parameter(Mandatory=$true)][string]$SourceData, [Parameter(Mandatory=$true)][string]$Destination, [switch]$ConfirmSourceStopped)
$ErrorActionPreference = 'Stop'
if (-not $ConfirmSourceStopped) { throw 'Stop Node writers and pass -ConfirmSourceStopped before copying' }
$source = (Resolve-Path -LiteralPath $SourceData).Path.TrimEnd('\','/')
$target = [IO.Path]::GetFullPath($Destination).TrimEnd('\','/')
if (Test-Path -LiteralPath $target) { throw 'Destination must be a new directory' }
if ($target.StartsWith($source + [IO.Path]::DirectorySeparatorChar, [StringComparison]::OrdinalIgnoreCase)) { throw 'Destination must be outside source data' }
New-Item -ItemType Directory -Path $target | Out-Null
& robocopy $source $target /E /COPY:DAT /DCOPY:DAT /XJ /R:1 /W:1 /NFL /NDL /NP
if ($LASTEXITCODE -ge 8) { throw "Copy failed with robocopy exit $LASTEXITCODE" }
Write-Output "Copied Node data to $target. This is a rehearsal copy, not a schema migration. Use a separate Go database and port."
exit 0

# Usage:
#   .\build.ps1                # build everything into .\dist\*.zip
#   .\build.ps1 windows-x64    # build a single target

[CmdletBinding()]
param(
    [string]$Target = ""
)

$ErrorActionPreference = 'Stop'

$targets = @(
    @{ label = "windows-x64";   goos = "windows"; goarch = "amd64"; ext = ".exe" }
    @{ label = "windows-x86";   goos = "windows"; goarch = "386";   ext = ".exe" }
    @{ label = "linux-x64";     goos = "linux";   goarch = "amd64"; ext = ""     }
    @{ label = "linux-arm64";   goos = "linux";   goarch = "arm64"; ext = ""     }
    @{ label = "darwin-x64";    goos = "darwin";  goarch = "amd64"; ext = ""     }
    @{ label = "darwin-arm64";  goos = "darwin";  goarch = "arm64"; ext = ""     }
)

$outDir = if ($env:OUT_DIR) { $env:OUT_DIR } else { "dist" }
New-Item -ItemType Directory -Force -Path $outDir | Out-Null

$version = if ($env:VERSION) {
    $env:VERSION
} else {
    try { (& git rev-parse --short HEAD 2>$null).Trim() } catch { "dev" }
}
if (-not $version) { $version = "dev" }

$date = (Get-Date).ToUniversalTime().ToString("yyyy-MM-ddTHH:mm:ssZ")
$ldflags = "-s -w -X main.Version=$version -X main.BuildDate=$date"

foreach ($t in $targets) {
    if ($Target -and $Target -ne $t.label) { continue }

    $bin     = "bpm$($t.ext)"
    $stage   = Join-Path $outDir ".stage-$($t.label)"
    $zipName = "bpm-$($t.label).zip"
    $zipPath = Join-Path $outDir $zipName

    if (Test-Path $stage) { Remove-Item -Recurse -Force $stage }
    New-Item -ItemType Directory -Force -Path $stage | Out-Null

    $env:CGO_ENABLED = "0"
    $env:GOOS        = $t.goos
    $env:GOARCH      = $t.goarch

    & go build -trimpath -ldflags $ldflags -o (Join-Path $stage $bin) .
    if ($LASTEXITCODE -ne 0) {
        throw "build failed for $($t.label)"
    }

    if (Test-Path $zipPath) { Remove-Item $zipPath }
    Compress-Archive -Path (Join-Path $stage $bin) -DestinationPath $zipPath -CompressionLevel Optimal

    Remove-Item -Recurse -Force $stage

    $size = (Get-Item $zipPath).Length
    Write-Host ("==> {0,-14} -> {1} ({2:N0} bytes)" -f $t.label, $zipPath, $size)
}

Remove-Item env:CGO_ENABLED, env:GOOS, env:GOARCH -ErrorAction SilentlyContinue

Write-Host ""
Write-Host "Done. Artifacts in: $outDir\"
Get-ChildItem (Join-Path $outDir "*.zip") |
    Select-Object Name, @{Name="Size";Expression={"{0:N0}" -f $_.Length}} |
    Format-Table -AutoSize

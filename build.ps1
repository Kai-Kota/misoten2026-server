# Goサーバーをビルドし、config.json と一緒に出力先へ置く。
#   ./build.ps1                           -> Server/bin/
#   ./build.ps1 -OutDir <Unityのビルド先>/Server  -> ビルドしたゲームの隣に配置
param(
    [string]$OutDir = (Join-Path $PSScriptRoot "bin")
)
$ErrorActionPreference = "Stop"

New-Item -ItemType Directory -Force $OutDir | Out-Null
Push-Location $PSScriptRoot
try {
    $env:GOOS = "windows"
    $env:GOARCH = "amd64"
    go build -o (Join-Path $OutDir "misoten_server.exe") ./cmd/server
    if ($LASTEXITCODE -ne 0) { throw "go build failed" }
    Copy-Item (Join-Path $PSScriptRoot "config.json") $OutDir -Force
    Write-Host "Built: $(Join-Path $OutDir 'misoten_server.exe')"
}
finally {
    Pop-Location
}

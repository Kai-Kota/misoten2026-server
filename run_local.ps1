# 1台のPCでゲームを複数起動して通信を試す。1つ目が -host でサーバーを起動し、残りは 127.0.0.1 に入る。
# 指定した人数が揃うと全員が自動で Ready になる。ウィンドウは画面に並べる。操作できるのはフォーカスのあるウィンドウだけ。
#   ./run_local.ps1                                   -> Builds/PrototypeOnline/PrototypeOnline.exe を4つ
#   ./run_local.ps1 -Count 2
#   ./run_local.ps1 -Exe ../misoten2026/Builds/KaiNetworkTest/KaiNetworkTest.exe
param(
    [string]$Exe = (Join-Path $PSScriptRoot "../misoten2026/Builds/PrototypeOnline/PrototypeOnline.exe"),
    [ValidateRange(1, 4)][int]$Count = 4
)
$ErrorActionPreference = "Stop"

$Exe = (Resolve-Path $Exe).Path
Add-Type -AssemblyName System.Windows.Forms
Add-Type @"
using System;
using System.Runtime.InteropServices;
public static class LocalRunWindow {
    [DllImport("user32.dll")] public static extern bool MoveWindow(IntPtr hWnd, int x, int y, int width, int height, bool repaint);
}
"@

# 画面の作業領域を 2x2 に分ける。
$area = [System.Windows.Forms.Screen]::PrimaryScreen.WorkingArea
$columns = if ($Count -eq 1) { 1 } else { 2 }
$rows = [Math]::Ceiling($Count / $columns)
$width = [int]($area.Width / $columns)
$height = [int]($area.Height / $rows)

$processes = @()
for ($index = 0; $index -lt $Count; $index++) {
    $arguments = @("-name", "P$($index + 1)", "-autoReady", $Count, "-screen-fullscreen", "0",
        "-screen-width", $width, "-screen-height", $height)
    $arguments = if ($index -eq 0) { @("-host") + $arguments } else { @("-connect", "127.0.0.1") + $arguments }
    $processes += Start-Process -FilePath $Exe -ArgumentList $arguments -PassThru
    # Host のサーバーが起動するのを待ってから残りを起動する（間に合わなくても再試行はしない）。
    Start-Sleep -Seconds $(if ($index -eq 0) { 3 } else { 1 })
}

for ($index = 0; $index -lt $processes.Count; $index++) {
    $process = $processes[$index]
    for ($wait = 0; $wait -lt 50 -and $process.MainWindowHandle -eq [IntPtr]::Zero; $wait++) {
        Start-Sleep -Milliseconds 200
        $process.Refresh()
    }
    $x = $area.X + ($index % $columns) * $width
    $y = $area.Y + [Math]::Floor($index / $columns) * $height
    [LocalRunWindow]::MoveWindow($process.MainWindowHandle, $x, $y, $width, $height, $true) | Out-Null
}
Write-Host "Started $Count clients. Close the P1 window last; it also stops the server."

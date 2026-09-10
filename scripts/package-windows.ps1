# Build a Windows zip: GoEzviz.exe + bundled FFmpeg.
$ErrorActionPreference = "Stop"

$ReleaseName = "1.0.0-with-ffmpeg-completed"
$root = Split-Path -Parent (Split-Path -Parent $MyInvocation.MyCommand.Path)
Set-Location $root

$ffmpegDir = Join-Path $root "resources\ffmpeg"
if (-not (Test-Path (Join-Path $ffmpegDir "ffmpeg.exe"))) {
    Write-Host "FFmpeg not found; downloading..."
    & (Join-Path $root "scripts\fetch-ffmpeg-windows.ps1")
}

go build -ldflags "-H=windowsgui -s -w" -o GoEzviz.exe .

$dist = Join-Path $root "dist\$ReleaseName"
if (Test-Path $dist) {
    Remove-Item -Recurse -Force $dist
}
New-Item -ItemType Directory -Force -Path (Join-Path $dist "ffmpeg") | Out-Null
Copy-Item GoEzviz.exe $dist
Copy-Item (Join-Path $ffmpegDir "ffmpeg.exe") (Join-Path $dist "ffmpeg\ffmpeg.exe")
Copy-Item (Join-Path $ffmpegDir "ffplay.exe") (Join-Path $dist "ffmpeg\ffplay.exe")
if (Test-Path (Join-Path $ffmpegDir "ffprobe.exe")) {
    Copy-Item (Join-Path $ffmpegDir "ffprobe.exe") (Join-Path $dist "ffmpeg\ffprobe.exe")
}

$zip = Join-Path $root "dist\GoEzviz-$ReleaseName-windows-amd64.zip"
if (Test-Path $zip) {
    Remove-Item -Force $zip
}
Compress-Archive -Path $dist -DestinationPath $zip
Write-Host "Created $zip"
Get-ChildItem $dist -Recurse -File | Format-Table FullName, @{N="MB";E={[math]::Round($_.Length/1MB,1)}} -AutoSize

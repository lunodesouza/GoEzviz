# Download Gyan essentials FFmpeg into resources/ffmpeg (Windows).
$ErrorActionPreference = "Stop"

$root = Split-Path -Parent (Split-Path -Parent $MyInvocation.MyCommand.Path)
$dest = Join-Path $root "resources\ffmpeg"
$zip = Join-Path $env:TEMP "ffmpeg-9.0-essentials.zip"
$url = "https://github.com/GyanD/codexffmpeg/releases/download/9.0/ffmpeg-9.0-essentials_build.zip"

New-Item -ItemType Directory -Force -Path $dest | Out-Null
Write-Host "Downloading $url ..."
& curl.exe -L --fail --retry 3 -o $zip $url

$extract = Join-Path $env:TEMP "ffmpeg-essentials-extract"
if (Test-Path $extract) {
    Remove-Item -Recurse -Force $extract
}
Expand-Archive -Path $zip -DestinationPath $extract -Force

$bin = Get-ChildItem -Path $extract -Recurse -Filter "ffmpeg.exe" | Select-Object -First 1
if (-not $bin) {
    throw "ffmpeg.exe not found in the downloaded archive"
}
$binDir = $bin.Directory.FullName
foreach ($name in @("ffmpeg.exe", "ffplay.exe", "ffprobe.exe")) {
    $src = Join-Path $binDir $name
    if (Test-Path $src) {
        Copy-Item -Force $src (Join-Path $dest $name)
        Write-Host "Copied $name"
    }
}

Remove-Item -Force $zip
Remove-Item -Recurse -Force $extract
Get-ChildItem $dest -Filter *.exe | Format-Table Name, @{N="MB";E={[math]::Round($_.Length/1MB,1)}} -AutoSize

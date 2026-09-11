$ErrorActionPreference = 'Stop'
$version = '3.3.4'
$expected = '8588238C9A5A00AA542906B6EC7E6D5541D9FFB9B5D0F6E1BC0E365E2303079E'
$toolsDirectory = Join-Path $PSScriptRoot '..\tools'
$destination = Join-Path $toolsDirectory "scrcpy-server-v$version"
New-Item -ItemType Directory -Path $toolsDirectory -Force | Out-Null
Invoke-WebRequest "https://github.com/Genymobile/scrcpy/releases/download/v$version/scrcpy-server-v$version" -OutFile $destination
$actual = (Get-FileHash $destination -Algorithm SHA256).Hash
if ($actual -ne $expected) {
    Remove-Item -LiteralPath $destination
    throw "scrcpy server checksum mismatch: $actual"
}
Write-Host "Verified $destination ($actual)"

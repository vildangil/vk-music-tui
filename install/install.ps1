# vktui installer for Windows.
#   irm https://raw.githubusercontent.com/vildangil/vk-music-tui/main/install/install.ps1 | iex
$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'
[Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12

$repo = 'vildangil/vk-music-tui'
$arch = if ($env:PROCESSOR_ARCHITECTURE -eq 'ARM64' -or $env:PROCESSOR_ARCHITEW6432 -eq 'ARM64') { 'arm64' } else { 'amd64' }
$url  = "https://github.com/$repo/releases/latest/download/vk-music-tui_windows_$arch.zip"
$dir  = Join-Path $env:LOCALAPPDATA 'Programs\vk-music-tui'

Write-Host "==> Скачиваю vktui ($arch)..." -ForegroundColor Cyan
$tmp = Join-Path $env:TEMP ("vktui-" + [guid]::NewGuid())
New-Item -ItemType Directory -Force $tmp | Out-Null
try {
    $zip = Join-Path $tmp 'vktui.zip'
    Invoke-WebRequest -UseBasicParsing $url -OutFile $zip
    Expand-Archive -Force $zip $tmp
    New-Item -ItemType Directory -Force $dir | Out-Null
    Get-Process vktui -ErrorAction SilentlyContinue | Stop-Process -Force -ErrorAction SilentlyContinue
    Copy-Item -Force (Join-Path $tmp 'vktui.exe') (Join-Path $dir 'vktui.exe')
} finally {
    Remove-Item -Recurse -Force $tmp -ErrorAction SilentlyContinue
}

$userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
if (-not $userPath) { $userPath = '' }
if (($userPath -split ';') -notcontains $dir) {
    [Environment]::SetEnvironmentVariable('Path', ($userPath.TrimEnd(';') + ';' + $dir).TrimStart(';'), 'User')
    Write-Host "==> Добавил $dir в PATH" -ForegroundColor Cyan
}
$env:Path = "$env:Path;$dir"

$mpvKnown = @(
    'C:\Program Files\MPV Player\mpv.exe',
    (Join-Path $env:LOCALAPPDATA 'Microsoft\WinGet\Links\mpv.exe'),
    (Join-Path $env:USERPROFILE 'scoop\shims\mpv.exe')
)
$hasMpv = (Get-Command mpv -ErrorAction SilentlyContinue) -or ($mpvKnown | Where-Object { Test-Path $_ })
if (-not $hasMpv) {
    if (Get-Command winget -ErrorAction SilentlyContinue) {
        Write-Host "==> Ставлю mpv (движок воспроизведения) через winget..." -ForegroundColor Cyan
        winget install --id shinchiro.mpv -e --accept-source-agreements --accept-package-agreements
    } elseif (Get-Command scoop -ErrorAction SilentlyContinue) {
        scoop install mpv
    } else {
        Write-Host "!! Не нашёл winget/scoop. Скачай mpv вручную: https://mpv.io/installation/ и положи mpv.exe в $dir" -ForegroundColor Yellow
    }
}

# Shortcuts: desktop + Start menu (skip with $env:VKTUI_NO_SHORTCUTS = "1")
if ($env:VKTUI_NO_SHORTCUTS -ne '1') {
    $ws = New-Object -ComObject WScript.Shell
    $targets = @(
        (Join-Path ([Environment]::GetFolderPath('Desktop')) 'VK Music TUI.lnk'),
        (Join-Path ([Environment]::GetFolderPath('Programs')) 'VK Music TUI.lnk')
    )
    foreach ($lnk in $targets) {
        $s = $ws.CreateShortcut($lnk)
        $s.TargetPath = Join-Path $dir 'vktui.exe'
        $s.WorkingDirectory = $dir
        $s.Description = 'VK Music TUI'
        $s.Save()
    }
    Write-Host "==> Ярлыки: рабочий стол и меню Пуск" -ForegroundColor Cyan
}

Write-Host ""
Write-Host "Готово! Открой НОВОЕ окно терминала и набери:  vktui" -ForegroundColor Green

$ErrorActionPreference = "Stop"

$root = Split-Path -Parent $MyInvocation.MyCommand.Path
$target = Join-Path $root "start-kiro-go-claude.bat"

if (-not (Test-Path $target)) {
  throw "Missing launcher: $target"
}

$desktop = [Environment]::GetFolderPath("Desktop")
$shortcutPath = Join-Path $desktop "Kiro-Go Claude Gateway.lnk"

$shell = New-Object -ComObject WScript.Shell
$shortcut = $shell.CreateShortcut($shortcutPath)
$shortcut.TargetPath = $target
$shortcut.WorkingDirectory = $root
$shortcut.WindowStyle = 7
$shortcut.Description = "Start Kiro-Go Claude Desktop gateway"
$shortcut.Save()

Write-Host "Created shortcut: $shortcutPath"

# Installs savesync for the current user and starts it at every logon.
# Run from the repo root after building: powershell -ExecutionPolicy Bypass -File scripts\install-windows.ps1
$ErrorActionPreference = 'Stop'

$src = Join-Path $PSScriptRoot '..\bin'
$dest = Join-Path $env:LOCALAPPDATA 'game-save-sync\bin'
$taskName = 'game-save-sync'

foreach ($exe in 'savesync.exe', 'savesyncw.exe') {
    if (-not (Test-Path (Join-Path $src $exe))) { throw "Missing bin\$exe - build it first." }
}

# Stop the running copy so its files can be replaced.
if (Get-ScheduledTask -TaskName $taskName -ErrorAction SilentlyContinue) {
    Stop-ScheduledTask -TaskName $taskName
}
Get-Process savesyncw -ErrorAction SilentlyContinue | Stop-Process -Force
Start-Sleep -Milliseconds 500

New-Item -ItemType Directory -Force -Path $dest | Out-Null
Copy-Item (Join-Path $src 'savesync.exe'), (Join-Path $src 'savesyncw.exe') $dest -Force

# Put the console build on the user PATH so `savesync games` works in any terminal.
$userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
if (($userPath -split ';') -notcontains $dest) {
    [Environment]::SetEnvironmentVariable('Path', ($userPath.TrimEnd(';') + ';' + $dest), 'User')
    Write-Host "Added $dest to your PATH (open a new terminal to use it)."
}

$user = "$env:USERDOMAIN\$env:USERNAME"
$action = New-ScheduledTaskAction -Execute (Join-Path $dest 'savesyncw.exe') -Argument 'run'
$trigger = New-ScheduledTaskTrigger -AtLogOn -User $user
$settings = New-ScheduledTaskSettingsSet `
    -ExecutionTimeLimit ([TimeSpan]::Zero) `
    -RestartCount 999 -RestartInterval (New-TimeSpan -Minutes 1) `
    -AllowStartIfOnBatteries -DontStopIfGoingOnBatteries `
    -MultipleInstances IgnoreNew
$principal = New-ScheduledTaskPrincipal -UserId $user -LogonType Interactive -RunLevel Limited

Register-ScheduledTask -TaskName $taskName -Action $action -Trigger $trigger -Settings $settings `
    -Principal $principal -Description 'Keeps non-Steam game saves in sync with the Steam Deck.' -Force | Out-Null
Start-ScheduledTask -TaskName $taskName

Write-Host "Installed. savesync is running and will start at every logon."
Write-Host "Log: $env:LOCALAPPDATA\game-save-sync\savesync.log"

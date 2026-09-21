param(
    [string]$ExePath = "",
    [string]$LogPath = ""
)

$ErrorActionPreference = "Stop"

$repoRoot = Split-Path -Parent $PSScriptRoot

if ([string]::IsNullOrWhiteSpace($ExePath)) {
    $candidates = @(
        (Join-Path $PSScriptRoot "glitchscope.exe"),
        (Join-Path $repoRoot "glitchscope.exe"),
        (Join-Path $repoRoot "dist\windows-amd64\glitchscope.exe")
    )

    $ExePath = $candidates |
        Where-Object { Test-Path -LiteralPath $_ -PathType Leaf } |
        Select-Object -First 1
}

if ([string]::IsNullOrWhiteSpace($ExePath) -or -not (Test-Path -LiteralPath $ExePath -PathType Leaf)) {
    throw "glitchscope.exe not found. Pass -ExePath C:\path\to\glitchscope.exe."
}

$ExePath = (Resolve-Path -LiteralPath $ExePath).Path
$workingDirectory = Split-Path -Parent $ExePath

if ([string]::IsNullOrWhiteSpace($LogPath)) {
    $logDirectory = Join-Path $workingDirectory "logs"
    New-Item -ItemType Directory -Force -Path $logDirectory | Out-Null
    $stamp = Get-Date -Format "yyyyMMdd-HHmmss"
    $LogPath = Join-Path $logDirectory "glitchscope-$stamp.log"
}

$LogPath = [System.IO.Path]::GetFullPath($LogPath)
$logParent = Split-Path -Parent $LogPath
if (-not [string]::IsNullOrWhiteSpace($logParent)) {
    New-Item -ItemType Directory -Force -Path $logParent | Out-Null
}

# Start-Process requires separate files for stdout and stderr. GlitchScope's
# slog handler writes to stderr; stdout is captured separately for completeness.
$stdoutPath = "$LogPath.stdout"
$arguments = @("-v")

Write-Host "Starting: $ExePath"
Write-Host "Log:     $LogPath"
Write-Host "Close the application window when the capture is complete."

$process = Start-Process `
    -FilePath $ExePath `
    -ArgumentList $arguments `
    -WorkingDirectory $workingDirectory `
    -RedirectStandardError $LogPath `
    -RedirectStandardOutput $stdoutPath `
    -PassThru `
    -Wait

Write-Host "Process exited with code $($process.ExitCode)."
Write-Host "Debug log saved to: $LogPath"

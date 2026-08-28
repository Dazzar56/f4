param(
    [string]$LogPath
)

$ErrorActionPreference = 'Continue'

if ([string]::IsNullOrWhiteSpace($LogPath)) {
    $stamp = Get-Date -Format 'yyyyMMdd-HHmmss'
    $LogPath = Join-Path (Get-Location) ("terminal-width-windows-{0}.log" -f $stamp)
}

$logDirectory = Split-Path -Parent $LogPath
if (-not [string]::IsNullOrWhiteSpace($logDirectory)) {
    New-Item -ItemType Directory -Force -Path $logDirectory | Out-Null
}

$transcriptStarted = $false
$testRoot = $null

function Section([string]$Name) {
    Write-Output ("===== {0} =====" -f $Name)
}

function Run-Cmd([string]$Name, [string]$CommandLine) {
    Section $Name
    Write-Output ("$ cmd.exe /d /c {0}" -f $CommandLine)
    & cmd.exe /d /c $CommandLine
    Write-Output ("[exit={0}]" -f $LASTEXITCODE)
}

function Run-PowerShell([string]$Name, [scriptblock]$Action) {
    Section $Name
    Write-Output ("$ PowerShell {0}" -f $Name)
    & $Action
    Write-Output ("[last-success={0}]" -f $?)
}

try {
    Start-Transcript -Path $LogPath -Force | Out-Null
    $transcriptStarted = $true

    Section 'META'
    Write-Output ("timestamp={0}" -f (Get-Date -Format o))
    Write-Output ("computer={0}" -f $env:COMPUTERNAME)
    Write-Output ("user={0}" -f $env:USERNAME)
    Write-Output ("os={0}" -f $env:OS)
    Write-Output ("process={0}" -f $PID)
    Write-Output ("powershell={0}" -f $PSVersionTable.PSVersion)
    Write-Output ("TERM={0}" -f $env:TERM)
    Write-Output ("WT_SESSION={0}" -f $env:WT_SESSION)
    Write-Output ("ConEmuANSI={0}" -f $env:ConEmuANSI)
    Write-Output ("COLUMNS={0}" -f $env:COLUMNS)
    Write-Output ("LINES={0}" -f $env:LINES)
    Write-Output ("Console.IsOutputRedirected={0}" -f [Console]::IsOutputRedirected)
    Write-Output ("Console.IsInputRedirected={0}" -f [Console]::IsInputRedirected)

    try {
        $raw = $Host.UI.RawUI
        Write-Output ("RawUI.WindowSize={0}x{1}" -f $raw.WindowSize.Width, $raw.WindowSize.Height)
        Write-Output ("RawUI.BufferSize={0}x{1}" -f $raw.BufferSize.Width, $raw.BufferSize.Height)
        Write-Output ("RawUI.CursorPosition={0},{1}" -f $raw.CursorPosition.X, $raw.CursorPosition.Y)
        Write-Output ("Console.WindowSize={0}x{1}" -f [Console]::WindowWidth, [Console]::WindowHeight)
        Write-Output ("Console.BufferSize={0}x{1}" -f [Console]::BufferWidth, [Console]::BufferHeight)
    } catch {
        Write-Output ("console-size-error={0}" -f $_.Exception.Message)
    }

    try {
        $osInfo = Get-CimInstance Win32_OperatingSystem
        Write-Output ("windows-caption={0}" -f $osInfo.Caption)
        Write-Output ("windows-version={0}" -f $osInfo.Version)
        Write-Output ("windows-build={0}" -f $osInfo.BuildNumber)
    } catch {
        Write-Output ("windows-info-error={0}" -f $_.Exception.Message)
    }

    Run-Cmd 'CONSOLE-MODE' 'mode con'
    Run-Cmd 'CMD-VERSION' 'ver'

    $git = Get-Command git.exe -ErrorAction SilentlyContinue
    Write-Output ("git-on-windows={0}" -f ([bool]$git))

    $testRoot = Join-Path ([IO.Path]::GetTempPath()) ("terminal-width-probe-{0}" -f ([guid]::NewGuid().ToString('N')))
    New-Item -ItemType Directory -Path $testRoot -Force | Out-Null

    $names = New-Object System.Collections.Generic.List[string]
    for ($i = 1; $i -le 120; $i++) {
        $names.Add(("file-{0:D3}-short.txt" -f $i))
    }
    for ($i = 1; $i -le 20; $i++) {
        $names.Add(("file-{0:D3}-{1}.txt" -f $i, ('x' * (35 + ($i % 15)))))
    }
    $names.Add('file with spaces 01.txt')
    $names.Add('русское-имя-01.txt')

    foreach ($name in $names) {
        New-Item -ItemType File -Path (Join-Path $testRoot $name) -Force | Out-Null
    }
    Write-Output ("test-directory={0}" -f $testRoot)
    Write-Output ("test-file-count={0}" -f $names.Count)

    Run-Cmd 'DIR-W' ('dir /w "{0}"' -f $testRoot)
    Run-Cmd 'DIR-D' ('dir /d "{0}"' -f $testRoot)
    Run-Cmd 'DIR-B' ('dir /b "{0}"' -f $testRoot)

    Run-PowerShell 'FORMAT-WIDE' {
        Get-ChildItem -LiteralPath $testRoot | Format-Wide -AutoSize
    }
    Run-PowerShell 'FORMAT-TABLE' {
        Get-ChildItem -LiteralPath $testRoot | Format-Table -AutoSize Name, Length, FullName
    }
    Run-PowerShell 'PROCESS-TABLE' {
        Get-Process | Select-Object -First 20 | Format-Table -AutoSize
    }
    Run-PowerShell 'OUT-STRING-DEFAULT-WIDTH' {
        Get-ChildItem -LiteralPath $testRoot | Out-String
    }

    Section 'END'
    Write-Output 'Compare this log with a run in an ordinary terminal and with a run inside f4.'
    Write-Output 'The important values are RawUI/Console width, mode con, and the column layout above.'
} finally {
    if ($transcriptStarted) {
        Stop-Transcript | Out-Null
    }
    if ($null -ne $testRoot -and (Test-Path -LiteralPath $testRoot)) {
        Remove-Item -LiteralPath $testRoot -Recurse -Force -ErrorAction SilentlyContinue
    }
}

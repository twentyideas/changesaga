# Review Saga installer for Windows PowerShell 5.1+ and PowerShell 7+.
#
#   irm https://raw.githubusercontent.com/review-saga/review-saga/main/scripts/install.ps1 | iex
#
# The installer downloads the matching GitHub Release asset, verifies it against
# SHA256SUMS, and installs review-saga.exe for the current user. It never embeds
# credentials, requests elevation, or weakens PowerShell execution policy.
[CmdletBinding()]
param(
    [string]$Version = $env:REVIEW_SAGA_VERSION,
    [string]$InstallDir = $env:REVIEW_SAGA_INSTALL_DIR,
    [string]$Repo = $(if ($env:REVIEW_SAGA_REPO) { $env:REVIEW_SAGA_REPO } else { "review-saga/review-saga" }),
    [switch]$DryRun,
    [switch]$NoPathUpdate
)

$ErrorActionPreference = "Stop"
$ProgressPreference = "SilentlyContinue"
Set-StrictMode -Version 2.0

function Fail {
    param([string]$Message)
    throw "Review Saga installation failed: $Message"
}

if ($env:OS -ne "Windows_NT") {
    Fail "this installer requires Windows"
}
if ($Repo -notmatch '^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$') {
    Fail "invalid GitHub repository: $Repo"
}

# Windows PowerShell 5.1 can otherwise negotiate an obsolete TLS version.
if ([Net.ServicePointManager]::SecurityProtocol -band [Net.SecurityProtocolType]::Tls12) {
    # TLS 1.2 is already available.
} else {
    [Net.ServicePointManager]::SecurityProtocol =
        [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12
}

$NativeArchitecture = if ($env:PROCESSOR_ARCHITEW6432) {
    $env:PROCESSOR_ARCHITEW6432
} else {
    $env:PROCESSOR_ARCHITECTURE
}
switch ($NativeArchitecture.ToUpperInvariant()) {
    "AMD64" { $Architecture = "amd64" }
    "ARM64" { $Architecture = "arm64" }
    default { Fail "unsupported Windows architecture: $NativeArchitecture" }
}

$Headers = @{
    Accept = "application/vnd.github+json"
    "User-Agent" = "review-saga-installer"
}

if ([string]::IsNullOrWhiteSpace($Version) -or $Version -eq "latest") {
    $LatestRelease = Invoke-RestMethod -Uri "https://api.github.com/repos/$Repo/releases/latest" -Headers $Headers
    $Tag = [string]$LatestRelease.tag_name
    if ([string]::IsNullOrWhiteSpace($Tag)) {
        Fail "GitHub did not return a latest release tag"
    }
} elseif ($Version.StartsWith("v")) {
    $Tag = $Version
} else {
    $Tag = "v$Version"
}

if ($Tag -notmatch '^v[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?$') {
    Fail "invalid release tag: $Tag"
}

$PlainVersion = $Tag.Substring(1)
$ArchiveName = "review-saga_${PlainVersion}_windows_${Architecture}.zip"
$ReleaseBase = "https://github.com/$Repo/releases/download/$Tag"

if ([string]::IsNullOrWhiteSpace($InstallDir)) {
    $LocalAppData = [Environment]::GetFolderPath([Environment+SpecialFolder]::LocalApplicationData)
    if ([string]::IsNullOrWhiteSpace($LocalAppData)) {
        $InstallDir = Join-Path $HOME ".local\bin"
    } else {
        $InstallDir = Join-Path $LocalAppData "Programs\ReviewSaga\bin"
    }
}
$InstallDir = [IO.Path]::GetFullPath($InstallDir)

$WorkDir = Join-Path ([IO.Path]::GetTempPath()) ("review-saga-install-" + [guid]::NewGuid().ToString("N"))
New-Item -ItemType Directory -Path $WorkDir | Out-Null

try {
    $ArchivePath = Join-Path $WorkDir $ArchiveName
    $ChecksumsPath = Join-Path $WorkDir "SHA256SUMS"

    Write-Host "Review Saga $Tag for windows/$Architecture"
    Write-Host "Downloading $ArchiveName"
    Invoke-WebRequest -UseBasicParsing -Uri "$ReleaseBase/$ArchiveName" -OutFile $ArchivePath
    Invoke-WebRequest -UseBasicParsing -Uri "$ReleaseBase/SHA256SUMS" -OutFile $ChecksumsPath

    $ExpectedHash = $null
    foreach ($Line in Get-Content -LiteralPath $ChecksumsPath) {
        $ChecksumMatch = [regex]::Match($Line, '^([0-9A-Fa-f]{64})\s+\*?(.+)$')
        if ($ChecksumMatch.Success -and $ChecksumMatch.Groups[2].Value.Trim() -eq $ArchiveName) {
            $ExpectedHash = $ChecksumMatch.Groups[1].Value.ToLowerInvariant()
            break
        }
    }
    if (-not $ExpectedHash) {
        Fail "SHA256SUMS has no entry for $ArchiveName"
    }

    $ActualHash = (Get-FileHash -Algorithm SHA256 -LiteralPath $ArchivePath).Hash.ToLowerInvariant()
    if ($ActualHash -ne $ExpectedHash) {
        Fail "checksum mismatch for $ArchiveName (expected $ExpectedHash, got $ActualHash)"
    }
    Write-Host "Checksum verified: $ActualHash"

    $ExtractDir = Join-Path $WorkDir "unpacked"
    Expand-Archive -LiteralPath $ArchivePath -DestinationPath $ExtractDir
    $DownloadedBinary = Join-Path $ExtractDir "review-saga.exe"
    if (-not (Test-Path -LiteralPath $DownloadedBinary -PathType Leaf)) {
        Fail "archive does not contain review-saga.exe"
    }

    $VersionOutput = & $DownloadedBinary version 2>&1
    if ($LASTEXITCODE -ne 0) {
        Fail "the downloaded binary did not run on this machine"
    }
    Write-Host "Verified $VersionOutput"

    if ($DryRun) {
        Write-Host "Dry run complete; nothing was installed"
        return
    }

    New-Item -ItemType Directory -Force -Path $InstallDir | Out-Null
    $Destination = Join-Path $InstallDir "review-saga.exe"
    $Staged = Join-Path $InstallDir (".review-saga.install." + $PID + ".exe")
    Copy-Item -LiteralPath $DownloadedBinary -Destination $Staged -Force
    Move-Item -LiteralPath $Staged -Destination $Destination -Force

    if (-not $NoPathUpdate) {
        $UserPath = [Environment]::GetEnvironmentVariable("Path", "User")
        $NormalizedInstallDir = $InstallDir.TrimEnd('\')
        $AlreadyOnPath = @($UserPath -split ';') | Where-Object {
            $_ -and $_.Trim().TrimEnd('\') -ieq $NormalizedInstallDir
        }
        if (-not $AlreadyOnPath) {
            $UpdatedPath = if ([string]::IsNullOrWhiteSpace($UserPath)) {
                $InstallDir
            } else {
                "$UserPath;$InstallDir"
            }
            [Environment]::SetEnvironmentVariable("Path", $UpdatedPath, "User")
            Write-Host "Added $InstallDir to your user PATH"
        }
        if (-not (@($env:Path -split ';') | Where-Object { $_ -and $_.Trim().TrimEnd('\') -ieq $NormalizedInstallDir })) {
            $env:Path = "$InstallDir;$env:Path"
        }
    }

    Write-Host ""
    Write-Host "Review Saga installed successfully"
    Write-Host "Command: $Destination"
    Write-Host "Open a new terminal, then run: review-saga help"
} finally {
    if (Test-Path -LiteralPath $WorkDir) {
        Remove-Item -LiteralPath $WorkDir -Recurse -Force
    }
}

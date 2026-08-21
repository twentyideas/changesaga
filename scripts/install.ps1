# Change Saga installer for Windows PowerShell 5.1+ and PowerShell 7+.
#
#   irm https://raw.githubusercontent.com/twentyideas/changesaga/main/scripts/install.ps1 | iex
#
# The installer downloads the matching GitHub Release asset, verifies it against
# SHA256SUMS, and installs change-saga.exe for the current user. It never embeds
# credentials, requests elevation, or weakens PowerShell execution policy.
[CmdletBinding()]
param(
    [string]$Version = $env:CHANGE_SAGA_VERSION,
    [string]$InstallDir = $env:CHANGE_SAGA_INSTALL_DIR,
    [string]$Repo = $(if ($env:CHANGE_SAGA_REPO) { $env:CHANGE_SAGA_REPO } else { "twentyideas/changesaga" }),
    [switch]$DryRun,
    [switch]$NoPathUpdate
)

$ErrorActionPreference = "Stop"
$ProgressPreference = "SilentlyContinue"
Set-StrictMode -Version 2.0

function Fail {
    param([string]$Message)
    throw "Change Saga installation failed: $Message"
}

if ($env:OS -ne "Windows_NT") {
    Fail "this installer requires Windows"
}
if ($Repo -notmatch '^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$') {
    Fail "invalid GitHub repository: $Repo"
}
$RepoComponents = $Repo.Split('/')
if ($RepoComponents[0] -in @('.', '..') -or $RepoComponents[1] -in @('.', '..')) {
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
    "User-Agent" = "change-saga-installer"
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

$SemVerIdentifier = '(?:0|[1-9][0-9]*|[0-9A-Za-z-]*[A-Za-z-][0-9A-Za-z-]*)'
$SemVerTagPattern = '^v(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)' +
    '(?:-' + $SemVerIdentifier + '(?:\.' + $SemVerIdentifier + ')*)?' +
    '(?:\+[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?\z'
if ($Tag -cnotmatch $SemVerTagPattern) {
    Fail "invalid release tag: $Tag"
}

$PlainVersion = $Tag.Substring(1)
$ArchiveName = "change-saga_${PlainVersion}_windows_${Architecture}.zip"
$ReleaseBase = "https://github.com/$Repo/releases/download/$Tag"

if ([string]::IsNullOrWhiteSpace($InstallDir)) {
    $LocalAppData = [Environment]::GetFolderPath([Environment+SpecialFolder]::LocalApplicationData)
    if ([string]::IsNullOrWhiteSpace($LocalAppData)) {
        $InstallDir = Join-Path $HOME ".local\bin"
    } else {
        $InstallDir = Join-Path $LocalAppData "Programs\ChangeSaga\bin"
    }
}
$InstallDir = [IO.Path]::GetFullPath($InstallDir)
if ($InstallDir.Contains(';')) {
    Fail "install directory cannot contain ';' because it cannot be represented safely in PATH"
}

$WorkDir = Join-Path ([IO.Path]::GetTempPath()) ("change-saga-install-" + [guid]::NewGuid().ToString("N"))
New-Item -ItemType Directory -Path $WorkDir | Out-Null
$Staged = $null

try {
    $ArchivePath = Join-Path $WorkDir $ArchiveName
    $ChecksumsPath = Join-Path $WorkDir "SHA256SUMS"

    Write-Host "Change Saga $Tag for windows/$Architecture"
    Write-Host "Downloading $ArchiveName"
    Invoke-WebRequest -UseBasicParsing -Uri "$ReleaseBase/$ArchiveName" -OutFile $ArchivePath
    Invoke-WebRequest -UseBasicParsing -Uri "$ReleaseBase/SHA256SUMS" -OutFile $ChecksumsPath

    $MatchingHashes = @()
    foreach ($Line in Get-Content -LiteralPath $ChecksumsPath) {
        # Accept the two standard sha256sum forms only: two spaces before a
        # text-mode name, or one space plus '*' before a binary-mode name.
        $ChecksumMatch = [regex]::Match($Line, '^([0-9A-Fa-f]{64}) [ *](.+)$')
        if ($ChecksumMatch.Success -and $ChecksumMatch.Groups[2].Value -ceq $ArchiveName) {
            $MatchingHashes += $ChecksumMatch.Groups[1].Value.ToLowerInvariant()
        }
    }
    if ($MatchingHashes.Count -ne 1) {
        Fail "SHA256SUMS must contain exactly one well-formed entry for $ArchiveName"
    }
    $ExpectedHash = $MatchingHashes[0]

    $ActualHash = (Get-FileHash -Algorithm SHA256 -LiteralPath $ArchivePath).Hash.ToLowerInvariant()
    if ($ActualHash -ne $ExpectedHash) {
        Fail "checksum mismatch for $ArchiveName (expected $ExpectedHash, got $ActualHash)"
    }
    Write-Host "Checksum verified: $ActualHash"

    # Do not use Expand-Archive on an uninspected release. A checksum identifies
    # the GitHub asset, but the ZIP still needs a safe and predictable layout.
    # Validate exactly the layout emitted by build-release.sh and copy only the
    # binary entry through a stream, so traversal entries are never materialized.
    Add-Type -AssemblyName System.IO.Compression.FileSystem
    $ExpectedEntries = @("change-saga.exe", "LICENSE", "README.md")
    $SeenEntries = @()
    $BinaryEntry = $null
    $Zip = [IO.Compression.ZipFile]::OpenRead($ArchivePath)
    try {
        foreach ($Entry in $Zip.Entries) {
            if (-not ($ExpectedEntries -ccontains $Entry.FullName)) {
                Fail "archive layout is invalid; unexpected entry: $($Entry.FullName)"
            }
            if ($SeenEntries -ccontains $Entry.FullName) {
                Fail "archive layout is invalid; duplicate entry: $($Entry.FullName)"
            }
            if ($Entry.Name -cne $Entry.FullName) {
                Fail "archive layout is invalid; entries must be regular files at the archive root"
            }
            $SeenEntries += $Entry.FullName
            if ($Entry.FullName -ceq "change-saga.exe") {
                $BinaryEntry = $Entry
            }
        }
        if ($SeenEntries.Count -ne $ExpectedEntries.Count) {
            Fail "archive layout is invalid; expected only change-saga.exe, LICENSE, and README.md at the archive root"
        }
        foreach ($ExpectedEntry in $ExpectedEntries) {
            if (-not ($SeenEntries -ccontains $ExpectedEntry)) {
                Fail "archive layout is invalid; missing entry: $ExpectedEntry"
            }
        }

        $ExtractDir = Join-Path $WorkDir "unpacked"
        New-Item -ItemType Directory -Path $ExtractDir | Out-Null
        $DownloadedBinary = Join-Path $ExtractDir "change-saga.exe"
        $InputStream = $BinaryEntry.Open()
        try {
            $OutputStream = [IO.File]::Open(
                $DownloadedBinary,
                [IO.FileMode]::CreateNew,
                [IO.FileAccess]::Write,
                [IO.FileShare]::None
            )
            try {
                $InputStream.CopyTo($OutputStream)
            } finally {
                $OutputStream.Dispose()
            }
        } finally {
            $InputStream.Dispose()
        }
    } finally {
        $Zip.Dispose()
    }

    $DownloadedBinary = Join-Path $ExtractDir "change-saga.exe"
    if (-not (Test-Path -LiteralPath $DownloadedBinary -PathType Leaf)) {
        Fail "archive did not contain change-saga.exe as a regular file"
    }

    $VersionOutput = & $DownloadedBinary version 2>&1
    if ($LASTEXITCODE -ne 0) {
        Fail "the downloaded binary did not run on this machine"
    }
    $VersionText = [string]::Join([Environment]::NewLine, [string[]]$VersionOutput).Trim()
    $ExpectedVersionPattern = '^' + [regex]::Escape($PlainVersion) + '(?: \([^\r\n]*\))?(?: built [^\r\n]+)?\z'
    if ($VersionText -cnotmatch $ExpectedVersionPattern) {
        Fail "release $Tag contained a binary reporting an unexpected version: $VersionText"
    }
    Write-Host "Verified $VersionText"

    if ($DryRun) {
        Write-Host "Dry run complete; nothing was installed"
        return
    }

    New-Item -ItemType Directory -Force -Path $InstallDir | Out-Null
    $Destination = Join-Path $InstallDir "change-saga.exe"
    if (Test-Path -LiteralPath $Destination -PathType Container) {
        Fail "install destination is a directory: $Destination"
    }
    if (Test-Path -LiteralPath $Destination) {
        $DestinationItem = Get-Item -LiteralPath $Destination -Force
        if ($DestinationItem.Attributes -band [IO.FileAttributes]::ReparsePoint) {
            Fail "refusing to replace a reparse-point install destination: $Destination"
        }
    }
    $Staged = Join-Path $InstallDir (".change-saga.install." + [guid]::NewGuid().ToString("N") + ".exe")
    [IO.File]::Copy($DownloadedBinary, $Staged, $false)
    try {
        if ([IO.File]::Exists($Destination)) {
            # File.Replace is an atomic same-volume swap and preserves the existing
            # binary if replacement fails.
            [IO.File]::Replace($Staged, $Destination, $null)
        } else {
            [IO.File]::Move($Staged, $Destination)
        }
    } catch {
        Fail "could not atomically replace ${Destination}: $($_.Exception.Message)"
    }
    $Staged = $null

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
            try {
                [Environment]::SetEnvironmentVariable("Path", $UpdatedPath, "User")
                Write-Host "Added $InstallDir to your user PATH"
            } catch {
                Write-Warning "Change Saga was installed, but the user PATH could not be updated: $($_.Exception.Message)"
            }
        }
        if (-not (@($env:Path -split ';') | Where-Object { $_ -and $_.Trim().TrimEnd('\') -ieq $NormalizedInstallDir })) {
            $env:Path = "$InstallDir;$env:Path"
        }
    }

    Write-Host ""
    Write-Host "Change Saga installed successfully"
    Write-Host "Command: $Destination"
    Write-Host "Open a new terminal, then run: change-saga help"
} finally {
    if ($Staged -and (Test-Path -LiteralPath $Staged)) {
        Remove-Item -LiteralPath $Staged -Force -ErrorAction SilentlyContinue
    }
    if (Test-Path -LiteralPath $WorkDir) {
        Remove-Item -LiteralPath $WorkDir -Recurse -Force -ErrorAction SilentlyContinue
    }
}

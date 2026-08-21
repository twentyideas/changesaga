# End-to-end test for install.ps1 using a locally staged release and mocked
# GitHub downloads. Intended for the windows-latest CI runner.
$ErrorActionPreference = "Stop"
Set-StrictMode -Version 2.0

$RepoRoot = Split-Path -Parent $PSScriptRoot
$WorkDir = Join-Path ([IO.Path]::GetTempPath()) ("change-saga-windows-test-" + [guid]::NewGuid().ToString("N"))
$ReleaseDir = Join-Path $WorkDir "release"
$StageDir = Join-Path $WorkDir "stage"
$Version = "9.9.9"
$ArchiveName = "change-saga_${Version}_windows_amd64.zip"
$Installer = Join-Path $PSScriptRoot "install.ps1"
$Pass = 0
$Fail = 0

function Record {
    param([string]$Name, [bool]$Passed)
    if ($Passed) {
        Write-Host "ok   $Name"
        $script:Pass++
    } else {
        Write-Host "FAIL $Name"
        $script:Fail++
    }
}

function Expect-Failure {
    param([string]$Name, [scriptblock]$Action, [string]$Pattern)
    try {
        & $Action
        Record $Name $false
    } catch {
        Record $Name ($_.Exception.Message -match $Pattern)
    }
}

function Write-ReleaseChecksum {
    param([string]$Directory, [string]$Name)
    $ArchivePath = Join-Path $Directory $Name
    $Hash = (Get-FileHash -Algorithm SHA256 -LiteralPath $ArchivePath).Hash.ToLowerInvariant()
    Set-Content -LiteralPath (Join-Path $Directory "SHA256SUMS") -Value "$Hash  $Name" -Encoding ascii
}

function New-TestZip {
    param([string]$Path, [array]$Entries)
    if (Test-Path -LiteralPath $Path) {
        Remove-Item -LiteralPath $Path -Force
    }
    $Zip = [IO.Compression.ZipFile]::Open($Path, [IO.Compression.ZipArchiveMode]::Create)
    try {
        foreach ($Entry in $Entries) {
            [IO.Compression.ZipFileExtensions]::CreateEntryFromFile(
                $Zip,
                [string]($Entry.Source),
                [string]($Entry.Name),
                [IO.Compression.CompressionLevel]::Optimal
            ) | Out-Null
        }
    } finally {
        $Zip.Dispose()
    }
}

New-Item -ItemType Directory -Path $ReleaseDir, $StageDir | Out-Null

try {
    Write-Host "== building test release artifact"
    Add-Type -AssemblyName System.IO.Compression.FileSystem
    $env:CGO_ENABLED = "0"
    $env:GOOS = "windows"
    $env:GOARCH = "amd64"
    $Binary = Join-Path $StageDir "change-saga.exe"
    $Ldflags = "-s -w -X github.com/change-saga/change-saga/internal/cli.Version=$Version"
    & go build -trimpath -ldflags $Ldflags -o $Binary ./cmd/change-saga
    if ($LASTEXITCODE -ne 0) { throw "go build failed" }
    Copy-Item (Join-Path $RepoRoot "LICENSE"), (Join-Path $RepoRoot "README.md") -Destination $StageDir
    $Archive = Join-Path $ReleaseDir $ArchiveName
    Compress-Archive -Path (Join-Path $StageDir "*") -DestinationPath $Archive
    Write-ReleaseChecksum $ReleaseDir $ArchiveName

    $script:MockReleaseDir = $ReleaseDir
    $script:MockLatestTag = "v$Version"
    function global:Invoke-WebRequest {
        param([switch]$UseBasicParsing, [string]$Uri, [string]$OutFile)
        $Leaf = [IO.Path]::GetFileName(([Uri]$Uri).AbsolutePath)
        $Source = Join-Path $script:MockReleaseDir $Leaf
        if (-not (Test-Path -LiteralPath $Source -PathType Leaf)) {
            throw "mock download missing: $Leaf"
        }
        Copy-Item -LiteralPath $Source -Destination $OutFile
    }
    function global:Invoke-RestMethod {
        param([string]$Uri, [hashtable]$Headers)
        return [pscustomobject]@{ tag_name = $script:MockLatestTag }
    }

    Write-Host "== happy path"
    $BinDir = Join-Path $WorkDir "bin"
    & $Installer -Version "v$Version" -InstallDir $BinDir -NoPathUpdate
    Record "installed change-saga.exe" (Test-Path -LiteralPath (Join-Path $BinDir "change-saga.exe") -PathType Leaf)
    $InstalledVersion = & (Join-Path $BinDir "change-saga.exe") version
    Record "installed binary runs" ($LASTEXITCODE -eq 0 -and -not [string]::IsNullOrWhiteSpace($InstalledVersion))

    Write-Host "== reinstall"
    & $Installer -Version "v$Version" -InstallDir $BinDir -NoPathUpdate
    Record "reinstall succeeds" (Test-Path -LiteralPath (Join-Path $BinDir "change-saga.exe") -PathType Leaf)

    Write-Host "== latest resolution"
    $LatestDir = Join-Path $WorkDir "latest"
    & $Installer -Version latest -InstallDir $LatestDir -NoPathUpdate
    Record "latest release installed" (Test-Path -LiteralPath (Join-Path $LatestDir "change-saga.exe") -PathType Leaf)

    Write-Host "== dry run"
    $DryDir = Join-Path $WorkDir "dry"
    & $Installer -Version "v$Version" -InstallDir $DryDir -DryRun -NoPathUpdate
    Record "dry run installs nothing" (-not (Test-Path -LiteralPath (Join-Path $DryDir "change-saga.exe")))

    Write-Host "== tampered archive"
    $TamperedDir = Join-Path $WorkDir "tampered"
    Copy-Item -Recurse $ReleaseDir $TamperedDir
    Add-Content -LiteralPath (Join-Path $TamperedDir $ArchiveName) -Value "tampered"
    $script:MockReleaseDir = $TamperedDir
    $BadDir = Join-Path $WorkDir "bad"
    New-Item -ItemType Directory -Path $BadDir | Out-Null
    Set-Content -LiteralPath (Join-Path $BadDir "change-saga.exe") -Value "existing install" -Encoding ascii
    Expect-Failure "checksum mismatch is rejected" {
        & $Installer -Version "v$Version" -InstallDir $BadDir -NoPathUpdate
    } "checksum mismatch"
    $Preserved = Get-Content -LiteralPath (Join-Path $BadDir "change-saga.exe") -Raw
    Record "existing install survives checksum failure" ($Preserved -match "existing install")

    Write-Host "== checksum manifest is unambiguous"
    $DuplicateDir = Join-Path $WorkDir "duplicate-checksum"
    Copy-Item -Recurse $ReleaseDir $DuplicateDir
    $ChecksumLine = Get-Content -LiteralPath (Join-Path $DuplicateDir "SHA256SUMS") -Raw
    Add-Content -LiteralPath (Join-Path $DuplicateDir "SHA256SUMS") -Value $ChecksumLine -Encoding ascii
    $script:MockReleaseDir = $DuplicateDir
    Expect-Failure "duplicate checksum entry is rejected" {
        & $Installer -Version "v$Version" -InstallDir (Join-Path $WorkDir "duplicate-install") -NoPathUpdate
    } "exactly one well-formed entry"

    $MalformedDir = Join-Path $WorkDir "malformed-checksum"
    Copy-Item -Recurse $ReleaseDir $MalformedDir
    Set-Content -LiteralPath (Join-Path $MalformedDir "SHA256SUMS") -Value "deadbeef  $ArchiveName" -Encoding ascii
    $script:MockReleaseDir = $MalformedDir
    Expect-Failure "malformed checksum entry is rejected" {
        & $Installer -Version "v$Version" -InstallDir (Join-Path $WorkDir "malformed-install") -NoPathUpdate
    } "exactly one well-formed entry"

    Write-Host "== archive layout is validated before extraction"
    $TraversalDir = Join-Path $WorkDir "traversal-release"
    New-Item -ItemType Directory -Path $TraversalDir | Out-Null
    $TraversalArchive = Join-Path $TraversalDir $ArchiveName
    New-TestZip $TraversalArchive @(
        @{ Source = $Binary; Name = "change-saga.exe" },
        @{ Source = (Join-Path $StageDir "LICENSE"); Name = "LICENSE" },
        @{ Source = (Join-Path $StageDir "README.md"); Name = "README.md" },
        @{ Source = (Join-Path $StageDir "README.md"); Name = "../escaped.exe" }
    )
    Write-ReleaseChecksum $TraversalDir $ArchiveName
    $script:MockReleaseDir = $TraversalDir
    $TraversalInstall = Join-Path $WorkDir "traversal-install"
    Expect-Failure "path-traversing ZIP entry is rejected" {
        & $Installer -Version "v$Version" -InstallDir $TraversalInstall -NoPathUpdate
    } "archive layout is invalid"
    Record "path-traversing entry was never extracted" (-not (Test-Path -LiteralPath (Join-Path $WorkDir "escaped.exe")))
    Record "invalid layout installed nothing" (-not (Test-Path -LiteralPath (Join-Path $TraversalInstall "change-saga.exe")))

    $DuplicateZipDir = Join-Path $WorkDir "duplicate-zip-release"
    New-Item -ItemType Directory -Path $DuplicateZipDir | Out-Null
    $DuplicateZipArchive = Join-Path $DuplicateZipDir $ArchiveName
    New-TestZip $DuplicateZipArchive @(
        @{ Source = $Binary; Name = "change-saga.exe" },
        @{ Source = $Binary; Name = "change-saga.exe" },
        @{ Source = (Join-Path $StageDir "LICENSE"); Name = "LICENSE" },
        @{ Source = (Join-Path $StageDir "README.md"); Name = "README.md" }
    )
    Write-ReleaseChecksum $DuplicateZipDir $ArchiveName
    $script:MockReleaseDir = $DuplicateZipDir
    Expect-Failure "duplicate binary ZIP entry is rejected" {
        & $Installer -Version "v$Version" -InstallDir (Join-Path $WorkDir "duplicate-zip-install") -NoPathUpdate
    } "duplicate entry"

    Write-Host "== release binary must match its tag"
    $WrongVersion = "9.9.8"
    $WrongArchiveName = "change-saga_${WrongVersion}_windows_amd64.zip"
    $WrongVersionDir = Join-Path $WorkDir "wrong-version-release"
    New-Item -ItemType Directory -Path $WrongVersionDir | Out-Null
    Copy-Item -LiteralPath $Archive -Destination (Join-Path $WrongVersionDir $WrongArchiveName)
    Write-ReleaseChecksum $WrongVersionDir $WrongArchiveName
    $script:MockReleaseDir = $WrongVersionDir
    Expect-Failure "mismatched binary version is rejected" {
        & $Installer -Version $WrongVersion -InstallDir (Join-Path $WorkDir "wrong-version-install") -NoPathUpdate
    } "unexpected version"

    Write-Host "== untrusted inputs fail before download"
    $script:MockReleaseDir = $ReleaseDir
    Expect-Failure "path-traversing version is rejected" {
        & $Installer -Version "9.9.9/../../escape" -InstallDir (Join-Path $WorkDir "bad-version") -NoPathUpdate
    } "invalid release tag"
    Expect-Failure "leading-zero SemVer core is rejected" {
        & $Installer -Version "v09.9.9" -InstallDir (Join-Path $WorkDir "leading-zero") -NoPathUpdate
    } "invalid release tag"
    Expect-Failure "empty prerelease identifier is rejected" {
        & $Installer -Version "v9.9.9-rc..1" -InstallDir (Join-Path $WorkDir "empty-identifier") -NoPathUpdate
    } "invalid release tag"
    Expect-Failure "repository metacharacters are rejected" {
        & $Installer -Repo "owner/repo;Write-Output-pwned" -Version "v$Version" -InstallDir (Join-Path $WorkDir "bad-repo") -NoPathUpdate
    } "invalid GitHub repository"
    Expect-Failure "dot repository component is rejected" {
        & $Installer -Repo "../repo" -Version "v$Version" -InstallDir (Join-Path $WorkDir "dot-repo") -NoPathUpdate
    } "invalid GitHub repository"
    Expect-Failure "PATH-delimiting install directory is rejected" {
        & $Installer -Version "v$Version" -InstallDir ((Join-Path $WorkDir "bad-path") + ";injected") -NoPathUpdate
    } "cannot contain"
    $script:MockLatestTag = "v09.9.9"
    Expect-Failure "untrusted latest tag is rejected" {
        & $Installer -Version latest -InstallDir (Join-Path $WorkDir "bad-latest") -NoPathUpdate
    } "invalid release tag"
    $script:MockLatestTag = "v$Version"

    Write-Host "== failed atomic swap preserves the existing install"
    $script:MockReleaseDir = $ReleaseDir
    $InstalledPath = Join-Path $BinDir "change-saga.exe"
    $LockedBinary = [IO.File]::Open($InstalledPath, [IO.FileMode]::Open, [IO.FileAccess]::Read, [IO.FileShare]::Read)
    try {
        Expect-Failure "locked destination rejects replacement" {
            & $Installer -Version "v$Version" -InstallDir $BinDir -NoPathUpdate
        } "could not atomically replace"
    } finally {
        $LockedBinary.Dispose()
    }
    $StillRuns = & $InstalledPath version
    Record "existing binary survives failed replacement" ($LASTEXITCODE -eq 0 -and $StillRuns -match "^$Version")
    $StagingFiles = @(Get-ChildItem -LiteralPath $BinDir -Filter ".change-saga.install.*" -Force)
    Record "failed replacement leaves no staging file" ($StagingFiles.Count -eq 0)

    Write-Host ""
    Write-Host "$Pass passed, $Fail failed"
    if ($Fail -ne 0) { throw "$Fail installer tests failed" }
} finally {
    Remove-Item function:global:Invoke-WebRequest -ErrorAction SilentlyContinue
    Remove-Item function:global:Invoke-RestMethod -ErrorAction SilentlyContinue
    Remove-Item -LiteralPath $WorkDir -Recurse -Force -ErrorAction SilentlyContinue
}

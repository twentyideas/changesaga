# End-to-end test for install.ps1 using a locally staged release and mocked
# GitHub downloads. Intended for the windows-latest CI runner.
$ErrorActionPreference = "Stop"
Set-StrictMode -Version 2.0

$RepoRoot = Split-Path -Parent $PSScriptRoot
$WorkDir = Join-Path ([IO.Path]::GetTempPath()) ("review-saga-windows-test-" + [guid]::NewGuid().ToString("N"))
$ReleaseDir = Join-Path $WorkDir "release"
$StageDir = Join-Path $WorkDir "stage"
$Version = "9.9.9"
$ArchiveName = "review-saga_${Version}_windows_amd64.zip"
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

New-Item -ItemType Directory -Path $ReleaseDir, $StageDir | Out-Null

try {
    Write-Host "== building test release artifact"
    $env:CGO_ENABLED = "0"
    $env:GOOS = "windows"
    $env:GOARCH = "amd64"
    $Binary = Join-Path $StageDir "review-saga.exe"
    & go build -trimpath -o $Binary ./cmd/review-saga
    if ($LASTEXITCODE -ne 0) { throw "go build failed" }
    Copy-Item (Join-Path $RepoRoot "LICENSE"), (Join-Path $RepoRoot "README.md") -Destination $StageDir
    $Archive = Join-Path $ReleaseDir $ArchiveName
    Compress-Archive -Path (Join-Path $StageDir "*") -DestinationPath $Archive
    $Hash = (Get-FileHash -Algorithm SHA256 -LiteralPath $Archive).Hash.ToLowerInvariant()
    Set-Content -LiteralPath (Join-Path $ReleaseDir "SHA256SUMS") -Value "$Hash  $ArchiveName" -Encoding ascii

    $script:MockReleaseDir = $ReleaseDir
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
        return [pscustomobject]@{ tag_name = "v$Version" }
    }

    Write-Host "== happy path"
    $BinDir = Join-Path $WorkDir "bin"
    & $Installer -Version "v$Version" -InstallDir $BinDir -NoPathUpdate
    Record "installed review-saga.exe" (Test-Path -LiteralPath (Join-Path $BinDir "review-saga.exe") -PathType Leaf)
    $InstalledVersion = & (Join-Path $BinDir "review-saga.exe") version
    Record "installed binary runs" ($LASTEXITCODE -eq 0 -and -not [string]::IsNullOrWhiteSpace($InstalledVersion))

    Write-Host "== reinstall"
    & $Installer -Version "v$Version" -InstallDir $BinDir -NoPathUpdate
    Record "reinstall succeeds" (Test-Path -LiteralPath (Join-Path $BinDir "review-saga.exe") -PathType Leaf)

    Write-Host "== latest resolution"
    $LatestDir = Join-Path $WorkDir "latest"
    & $Installer -Version latest -InstallDir $LatestDir -NoPathUpdate
    Record "latest release installed" (Test-Path -LiteralPath (Join-Path $LatestDir "review-saga.exe") -PathType Leaf)

    Write-Host "== dry run"
    $DryDir = Join-Path $WorkDir "dry"
    & $Installer -Version "v$Version" -InstallDir $DryDir -DryRun -NoPathUpdate
    Record "dry run installs nothing" (-not (Test-Path -LiteralPath (Join-Path $DryDir "review-saga.exe")))

    Write-Host "== tampered archive"
    $TamperedDir = Join-Path $WorkDir "tampered"
    Copy-Item -Recurse $ReleaseDir $TamperedDir
    Add-Content -LiteralPath (Join-Path $TamperedDir $ArchiveName) -Value "tampered"
    $script:MockReleaseDir = $TamperedDir
    $BadDir = Join-Path $WorkDir "bad"
    Expect-Failure "checksum mismatch is rejected" {
        & $Installer -Version "v$Version" -InstallDir $BadDir -NoPathUpdate
    } "checksum mismatch"
    Record "tampered binary was not installed" (-not (Test-Path -LiteralPath (Join-Path $BadDir "review-saga.exe")))

    Write-Host ""
    Write-Host "$Pass passed, $Fail failed"
    if ($Fail -ne 0) { throw "$Fail installer tests failed" }
} finally {
    Remove-Item function:global:Invoke-WebRequest -ErrorAction SilentlyContinue
    Remove-Item function:global:Invoke-RestMethod -ErrorAction SilentlyContinue
    Remove-Item -LiteralPath $WorkDir -Recurse -Force -ErrorAction SilentlyContinue
}

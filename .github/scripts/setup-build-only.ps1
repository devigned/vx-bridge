Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
foreach ($key in @('REVISION','WORKFLOW_REVISION','BUILD_INPUT_DIR','EVIDENCE_DIR',
    'NATIVE_ARTIFACT_ID','NATIVE_ARTIFACT_DIGEST','RUNNER_TEMP','GITHUB_RUN_ID','GITHUB_RUN_ATTEMPT','RELEASE_VERSION')) {
    if (-not [Environment]::GetEnvironmentVariable($key)) { throw "Missing $key" }
}
if ($env:OS -cne 'Windows_NT' -or -not [Environment]::Is64BitProcess -or
    $env:PROCESSOR_ARCHITECTURE -cne 'AMD64' -or
    $env:REVISION -cnotmatch '^[0-9a-f]{40}$' -or $env:WORKFLOW_REVISION -cnotmatch '^[0-9a-f]{40}$' -or
    $env:SETUP_BUILD_PROFILE -cne 'normal' -or
    $env:NATIVE_ARTIFACT_ID -cnotmatch '^[1-9][0-9]*$' -or
    $env:NATIVE_ARTIFACT_DIGEST -cnotmatch '^[0-9a-f]{64}$') { throw 'Exact normal Windows build identity required.' }
$source = (Resolve-Path (Join-Path $PSScriptRoot '..\..')).Path
$inputs = (Resolve-Path -LiteralPath $env:BUILD_INPUT_DIR).Path
$evidence = [IO.Path]::GetFullPath($env:EVIDENCE_DIR)
if ($evidence.StartsWith($source + '\',[StringComparison]::OrdinalIgnoreCase) -or
    $inputs.StartsWith($source + '\',[StringComparison]::OrdinalIgnoreCase)) { throw 'Outputs must be outside source.' }
if (Test-Path -LiteralPath $evidence) { throw 'New evidence directory required.' }
New-Item -ItemType Directory -Path $evidence | Out-Null
foreach ($relative in @('installer/build-setup.ps1','installer/native-tools.ps1',
    '.github/scripts/setup-build-only.ps1')) {
    $tokens=$null; $errors=$null
    [void][System.Management.Automation.Language.Parser]::ParseFile(
        (Join-Path $source $relative),[ref]$tokens,[ref]$errors)
    if (@($errors).Count) { throw "PowerShell parse failed: $relative $errors" }
}
. (Join-Path $source 'installer/native-tools.ps1')
$summary = [ordered]@{
    source_revision=$env:REVISION; workflow_revision=$env:WORKFLOW_REVISION
    release_version=$env:RELEASE_VERSION
    run_id=$env:GITHUB_RUN_ID; run_attempt=$env:GITHUB_RUN_ATTEMPT
    input_artifact_id=$env:NATIVE_ARTIFACT_ID; input_artifact_digest=$env:NATIVE_ARTIFACT_DIGEST
    success=$false; failed_tool=$null; native_exit_code=$null; failure=$null
    evidence_failure=$null
    package_code=$null; msi_sha256=$null
    warning_exceptions=@()
    ice_exceptions=@()
    scope='BUILD ONLY: author/COM/tables/CAB/static bytes; no installation, callback or product execution'
}
$toolLog = Join-Path $evidence 'native-tools.log'
$controlLog = Join-Path $evidence 'control-tools.log'
$staging = Join-Path $env:RUNNER_TEMP 'setup-windows-staging'
$output = Join-Path $env:RUNNER_TEMP 'setup-windows-authoring'
function Assert-ExactBuildSource {
    $head = & git -C $source rev-parse HEAD
    if ($LASTEXITCODE -ne 0 -or $head -cne $env:REVISION) { throw 'Source identity differs.' }
    $state = & git -C $source status --porcelain --untracked-files=all --ignored=matching
    if ($LASTEXITCODE -ne 0 -or $state) { throw 'Source status failed or checkout is dirty.' }
}
Start-Transcript -LiteralPath (Join-Path $evidence 'windows-build.log') | Out-Null
try {
    Assert-ExactBuildSource
    $identity=if ($env:RELEASE_VERSION -ceq 'development') { $env:REVISION } else { "$env:RELEASE_VERSION-$env:REVISION" }
    $archiveName = "vx-bridge-windows-amd64-$identity.zip"
    $hashed = @('setup.exe','setup-actions.dll','native-inputs.txt','native-SHA256SUMS',
        $archiveName,"$archiveName.sha256",'linux-payload.wxs','linux-setup-input.json',
        'compiler.txt','packages.txt','mingw-copyright.txt')
    $allowed = $hashed + @('INPUT-SHA256SUMS','provenance.json')
    $files = @(Get-ChildItem -LiteralPath $inputs -Force)
    if ($files.Count -ne $allowed.Count) { throw 'Unexpected input artifact file count.' }
    foreach ($file in $files) {
        if ($file.Name -cnotin $allowed -or $file.PSIsContainer -or $file.Length -gt 64MB -or
            ($file.Attributes -band [IO.FileAttributes]::ReparsePoint)) { throw 'Unexpected input artifact entry.' }
    }
    $seen = @{}
    foreach ($line in Get-Content -LiteralPath (Join-Path $inputs 'INPUT-SHA256SUMS')) {
        if ($line -cnotmatch '^([0-9a-f]{64})  ([^/\\]+)$') { throw 'Invalid input checksum.' }
        $hash = $Matches[1]; $name = $Matches[2]
        if ($name -cnotin $hashed -or $seen.ContainsKey($name) -or
            (Get-FileHash -LiteralPath (Join-Path $inputs $name) -Algorithm SHA256).Hash.ToLowerInvariant() -cne $hash) {
            throw 'Input checksum mismatch.'
        }
        $seen[$name]=$true
    }
    if ($seen.Count -ne $hashed.Count) { throw 'Missing input checksums.' }
    $provenance = Get-Content -Raw -LiteralPath (Join-Path $inputs 'provenance.json') | ConvertFrom-Json
    if ($provenance.schema -ne 1 -or $provenance.source_revision -cne $env:REVISION -or
        $provenance.release_version -cne $env:RELEASE_VERSION -or
        $provenance.workflow_revision -cne $env:WORKFLOW_REVISION -or
        $provenance.run_id -cne $env:GITHUB_RUN_ID -or $provenance.run_attempt -cne $env:GITHUB_RUN_ATTEMPT -or
        $provenance.platform -cne 'linux/amd64' -or
        $provenance.image -cne 'ubuntu@sha256:fe81560990fae86dc4743202b2cb4d3df98d0bcf949173cecef15ce1127ef852') {
        throw 'Cross-job input provenance mismatch.'
    }
    Copy-Item -LiteralPath (Join-Path $inputs 'provenance.json') -Destination (Join-Path $evidence 'input-provenance.json')
    $records = @(Get-Content -LiteralPath (Join-Path $inputs 'native-inputs.txt'))
    if ($records[0] -cne "source_revision=$env:REVISION") { throw 'Native source identity mismatch.' }
    $sourceHashes = @{}
    foreach ($line in $records) {
        if ($line -cmatch '^([0-9a-f]{64})  .+/(installer/(?:setup/[^/]+|managed/state\.[ch]))$') {
            $hash=$Matches[1]; $relative=$Matches[2]
            if ($sourceHashes.ContainsKey($relative) -or
                (Get-FileHash -LiteralPath (Join-Path $source $relative) -Algorithm SHA256).Hash.ToLowerInvariant() -cne $hash) {
                throw 'Native source bytes differ.'
            }
            $sourceHashes[$relative]=$true
        }
    }
    if ($sourceHashes.Count -ne 11) { throw 'Native source/resource inventory differs.' }
    $env:ARCHIVE=Join-Path $inputs $archiveName
    $env:NATIVE_DIR=$inputs
    $env:OUT=$staging
    Invoke-LoggedNativeTool -FilePath 'make' -LogPath $controlLog -Arguments @('-C',$source,'prepare-setup') | Out-Host
    foreach ($pair in @(@('payload.wxs','linux-payload.wxs'),@('setup-input.json','linux-setup-input.json'))) {
        if ((Get-FileHash -LiteralPath (Join-Path $staging $pair[0]) -Algorithm SHA256).Hash -cne
            (Get-FileHash -LiteralPath (Join-Path $inputs $pair[1]) -Algorithm SHA256).Hash) {
            throw 'Windows regeneration differs from exact Linux staging.'
        }
        $preparedMetadata = Get-Content -Raw -LiteralPath (Join-Path $staging 'setup-input.json') | ConvertFrom-Json
        $summary.warning_exceptions=@($preparedMetadata.warning_exceptions)
        $summary.ice_exceptions=@($preparedMetadata.ice_exceptions)
        if (($provenance.warning_exceptions | ConvertTo-Json -Compress -Depth 5) -cne
            ($preparedMetadata.warning_exceptions | ConvertTo-Json -Compress -Depth 5) -or
            ($provenance.ice_exceptions | ConvertTo-Json -Compress) -cne
            ($preparedMetadata.ice_exceptions | ConvertTo-Json -Compress)) { throw 'Input validation-policy provenance differs.' }
    }
    $pin = Get-Content -Raw -LiteralPath (Join-Path $source 'installer/toolchain.json') | ConvertFrom-Json
    if ($pin.wix_version -cne '3.14.1' -or
        $pin.wix_archive_url -cne 'https://github.com/wixtoolset/wix3/releases/download/wix3141rtm/wix314-binaries.zip' -or
        $pin.wix_archive_sha256 -cne '6ac824e1642d6f7277d0ed7ea09411a508f6116ba6fae0aa5f2c7daa2ff43d31') { throw 'Unapproved WiX pin.' }
    $wix = Join-Path $env:RUNNER_TEMP 'setup-wix314-binaries.zip'
    if (Test-Path -LiteralPath $wix) { throw 'WiX archive path already exists.' }
    Invoke-WebRequest -Uri $pin.wix_archive_url -OutFile $wix
    if ((Get-FileHash -LiteralPath $wix -Algorithm SHA256).Hash.ToLowerInvariant() -cne $pin.wix_archive_sha256) {
        throw 'WiX archive differs before use.'
    }
    $env:SETUP_STAGING=$staging; $env:WIX_ARCHIVE=$wix; $env:TOOL_LOG=$toolLog; $env:OUT=$output
    Invoke-LoggedNativeTool -FilePath 'make' -LogPath $controlLog -Arguments @('-C',$source,'build-setup-msi') | Out-Host
    $authored = Get-Content -Raw -LiteralPath (Join-Path $output 'setup-input.json') | ConvertFrom-Json
    $inspection = Get-Content -Raw -LiteralPath (Join-Path $output 'msi-inspection.json') | ConvertFrom-Json
    $msiHash = (Get-FileHash -LiteralPath (Join-Path $output 'bridge.msi') -Algorithm SHA256).Hash.ToLowerInvariant()
    $preparedHash = (Get-FileHash -LiteralPath (Join-Path $staging 'setup-input.json') -Algorithm SHA256).Hash.ToLowerInvariant()
    if ($authored.schema -ne 2 -or $authored.phase -cne 'authored-msi' -or
        $authored.build_profile -cne 'normal' -or $authored.version -cne $env:RELEASE_VERSION -or
        $authored.source_revision -cne $env:REVISION -or $inspection.source_revision -cne $env:REVISION -or
        $authored.package -cne $inspection.package -or $authored.msi_sha256 -cne $msiHash -or
        $inspection.msi_sha256 -cne $msiHash -or $authored.prepared_input_sha256 -cne $preparedHash -or
        $inspection.prepared_input_sha256 -cne $preparedHash) { throw 'Final emitted package identity is not bound to actual MSI/input bytes.' }
    $summary.package_code=$authored.package
    $summary.msi_sha256=$msiHash
    $env:AUTHORED_SETUP=$output
    $env:MINGW_COPYRIGHT=Join-Path $inputs 'mingw-copyright.txt'
    $env:OUT=Join-Path $env:RUNNER_TEMP 'release-artifacts'
    Invoke-LoggedNativeTool -FilePath 'make' -LogPath $controlLog -Arguments @('-C',$source,'package-installer') | Out-Host
    $summary.success=$true
} catch {
    $summary.failure=$_.Exception.Message
    if ($_.Exception.Data.Contains('NativeTool')) { $summary.failed_tool=$_.Exception.Data['NativeTool'] }
    if ($_.Exception.Data.Contains('NativeToolExitCode')) { $summary.native_exit_code=$_.Exception.Data['NativeToolExitCode'] }
    if (Test-Path -LiteralPath $toolLog -PathType Leaf) {
        foreach ($line in Get-Content -LiteralPath $toolLog) {
            if ($line -match '^\[exit\] tool=([^ ]+) code=(-?[0-9]+)$' -and [int]$Matches[2] -ne 0) {
                $summary.failed_tool=$Matches[1]; $summary.native_exit_code=[int]$Matches[2]
                break
            }
        }
    }
    throw
} finally {
    try {
        foreach ($name in @('actual.wxs','msi-inspection.json','setup-input.json')) {
            $path=Join-Path $output $name
            if (Test-Path -LiteralPath $path -PathType Leaf) { Copy-Item -LiteralPath $path -Destination $evidence }
        }
        $xml=Join-Path $staging 'payload.wxs'
        if (Test-Path -LiteralPath $xml -PathType Leaf) { Copy-Item -LiteralPath $xml -Destination $evidence }
        Assert-ExactBuildSource
    } catch {
        $summary.success=$false
        $summary.evidence_failure=$_.Exception.Message
        if (-not $summary.failure) { $summary.failure=$_.Exception.Message }
    }
    try {
        $summary | ConvertTo-Json -Depth 5 | Set-Content -Encoding utf8 -LiteralPath (Join-Path $evidence 'outcome.json')
    } finally { Stop-Transcript | Out-Null }
}
if (-not $summary.success) { throw 'Build-only verification failed; see retained outcome/logs.' }

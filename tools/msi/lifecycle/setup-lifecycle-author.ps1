Set-StrictMode -Version Latest
$ErrorActionPreference='Stop'
foreach ($key in @('REVISION','WORKFLOW_REVISION','BUILD_INPUT_DIR','LIFECYCLE_EVIDENCE',
    'GITHUB_RUN_ID','GITHUB_RUN_ATTEMPT','NATIVE_ARTIFACT_ID','NATIVE_ARTIFACT_DIGEST')) {
    if (-not [Environment]::GetEnvironmentVariable($key)) { throw "Missing $key" }
}
if ($env:OS -cne 'Windows_NT' -or $env:RUNNER_ENVIRONMENT -cne 'github-hosted' -or
    $env:GITHUB_EVENT_NAME -cne 'workflow_dispatch' -or $env:GITHUB_WORKFLOW -cne 'VX Bridge disposable lifecycle' -or
    $env:GITHUB_REPOSITORY -cne 'devigned/vx-bridge' -or
    $env:GITHUB_RUN_ATTEMPT -cne '1' -or $env:REVISION -cnotmatch '^[0-9a-f]{40}$' -or
    $env:REVISION -cne $env:WORKFLOW_REVISION) { throw 'Exact first-attempt manual hosted lifecycle scope required.' }
$source=(Resolve-Path (Join-Path $PSScriptRoot '..\..\..')).Path
$inputs=(Resolve-Path -LiteralPath $env:BUILD_INPUT_DIR).Path
$evidence=[IO.Path]::GetFullPath($env:LIFECYCLE_EVIDENCE)
if ($inputs.StartsWith($source+'\',[StringComparison]::OrdinalIgnoreCase) -or
    $evidence.StartsWith($source+'\',[StringComparison]::OrdinalIgnoreCase) -or
    (Test-Path -LiteralPath $evidence)) { throw 'New external lifecycle evidence required.' }
$head=& git -C $source rev-parse HEAD
if ($LASTEXITCODE -ne 0 -or $head -cne $env:REVISION) { throw 'Source identity differs.' }
$state=& git -C $source status --porcelain --untracked-files=all --ignored=matching
if ($LASTEXITCODE -ne 0 -or $state) { throw 'Source status failed or checkout is dirty.' }
New-Item -ItemType Directory -Path $evidence | Out-Null
. (Join-Path $source 'installer/native-tools.ps1')
$names=@(
    'native/setup.exe','native/setup-actions.dll','native/native-inputs.txt','native/native-SHA256SUMS',
    "versions/a/vx-bridge-windows-amd64-v0.0.1-$env:REVISION.zip",
    "versions/a/vx-bridge-windows-amd64-v0.0.1-$env:REVISION.zip.sha256",
    "versions/b/vx-bridge-windows-amd64-v0.0.2-$env:REVISION.zip",
    "versions/b/vx-bridge-windows-amd64-v0.0.2-$env:REVISION.zip.sha256",
    'lifecycle-worker.exe','worker-build-info.txt','compiler.txt','packages.txt','mingw-copyright.txt')
$allowed=$names+@('INPUT-SHA256SUMS','provenance.json')
$files=@(Get-ChildItem -LiteralPath $inputs -Force -Recurse)
foreach ($file in $files) {
    if ($file.Attributes -band [IO.FileAttributes]::ReparsePoint) { throw 'Reparse input refused.' }
    if (-not $file.PSIsContainer) {
        $relative=[IO.Path]::GetRelativePath($inputs,$file.FullName).Replace('\','/')
        if ($relative -cnotin $allowed -or $file.Length -gt 64MB) { throw 'Unexpected lifecycle input entry.' }
    }
}
if (@($files | Where-Object { -not $_.PSIsContainer }).Count -ne $allowed.Count) { throw 'Input count differs.' }
$seen=@{}
foreach ($line in Get-Content -LiteralPath (Join-Path $inputs 'INPUT-SHA256SUMS')) {
    if ($line -cnotmatch '^([0-9a-f]{64})  ([^\\]+)$') { throw 'Invalid checksum record.' }
    $hash=$Matches[1]; $name=$Matches[2]
    if ($name -cnotin $names -or $seen.ContainsKey($name) -or
        (Get-FileHash -LiteralPath (Join-Path $inputs $name) -Algorithm SHA256).Hash.ToLowerInvariant() -cne $hash) {
        throw 'Lifecycle input checksum differs.'
    }
    $seen[$name]=$true
}
if ($seen.Count -ne $names.Count) { throw 'Incomplete lifecycle checksums.' }
$provenance=Get-Content -Raw -LiteralPath (Join-Path $inputs 'provenance.json') | ConvertFrom-Json
if ($provenance.schema -ne 1 -or $provenance.source_revision -cne $env:REVISION -or
    $provenance.workflow_revision -cne $env:WORKFLOW_REVISION -or
    $provenance.run_id -cne $env:GITHUB_RUN_ID -or $provenance.run_attempt -cne '1' -or
    $provenance.platform -cne 'linux/amd64' -or
    $provenance.image -cne 'ubuntu@sha256:fe81560990fae86dc4743202b2cb4d3df98d0bcf949173cecef15ce1127ef852') {
    throw 'Lifecycle input source/run/platform identity differs.'
}
$records=@(Get-Content -LiteralPath (Join-Path $inputs 'native/native-inputs.txt'))
if ($records[0] -cne "source_revision=$env:REVISION") { throw 'Native source identity differs.' }
$sourceHashes=@{}
foreach ($line in $records) {
    if ($line -cmatch '^([0-9a-f]{64})  .+/(installer/(?:setup/[^/]+|managed/state\.[ch]))$') {
        $hash=$Matches[1]; $path=$Matches[2]
        if ($sourceHashes.ContainsKey($path) -or
            (Get-FileHash -LiteralPath (Join-Path $source $path) -Algorithm SHA256).Hash.ToLowerInvariant() -cne $hash) {
            throw 'Native source bytes differ.'
        }
        $sourceHashes[$path]=$true
    }
}
if ($sourceHashes.Count -ne 11) { throw 'Native source inventory differs.' }
$workerInfo=Get-Content -Raw -LiteralPath (Join-Path $inputs 'worker-build-info.txt')
$workerBytes=[IO.File]::ReadAllBytes((Join-Path $inputs 'lifecycle-worker.exe'))
if ($workerInfo -notlike '*github.com/devigned/vx-bridge/tools/msi/lifecycle*' -or
    $workerInfo -notlike '*GOOS=windows*' -or $workerInfo -notlike '*GOARCH=amd64*' -or $workerInfo -notlike '*CGO_ENABLED=0*' -or
    -not [Text.Encoding]::ASCII.GetString($workerBytes).Contains($env:REVISION)) {
    throw 'Actual private worker module/platform/embedded source stamp differs.'
}
$pin=Get-Content -Raw -LiteralPath (Join-Path $source 'installer/toolchain.json') | ConvertFrom-Json
if ($pin.wix_version -cne '3.14.1' -or $pin.wix_archive_sha256 -cne
    '6ac824e1642d6f7277d0ed7ea09411a508f6116ba6fae0aa5f2c7daa2ff43d31') { throw 'Unapproved WiX.' }
$wix=Join-Path $env:RUNNER_TEMP 'lifecycle-wix314-binaries.zip'
if (Test-Path -LiteralPath $wix) { throw 'WiX path already exists.' }
Invoke-WebRequest -Uri $pin.wix_archive_url -OutFile $wix
if ((Get-FileHash -LiteralPath $wix -Algorithm SHA256).Hash.ToLowerInvariant() -cne $pin.wix_archive_sha256) {
    throw 'WiX ZIP differs before use.'
}
$mediaRoot=Join-Path $env:RUNNER_TEMP 'lifecycle-test-media'
New-Item -ItemType Directory -Path $mediaRoot | Out-Null
$media=@()
$payloadNames=@('GSPconnect.exe','vxbridge.exe','LICENSE','THIRD_PARTY_NOTICES.txt','build-manifest.json','SHA256SUMS')
foreach ($role in @('a','b','b-fault')) {
    $version=if ($role -ceq 'a') { 'v0.0.1' } else { 'v0.0.2' }
    $archiveRole=if ($role -ceq 'a') { 'a' } else { 'b' }
    $profile=if ($role -ceq 'b-fault') { 'private-lifecycle-fault' } else { 'normal' }
    $archive=Join-Path $inputs "versions/$archiveRole/vx-bridge-windows-amd64-$version-$env:REVISION.zip"
    $stage=Join-Path $env:RUNNER_TEMP "lifecycle-stage-$role"
    $output=Join-Path $env:RUNNER_TEMP "lifecycle-authored-$role"
    $roleEvidence=Join-Path $evidence "author-$role"
    New-Item -ItemType Directory -Path $roleEvidence | Out-Null
    $env:RELEASE_VERSION=$version; $env:SETUP_BUILD_PROFILE=$profile
    $env:ARCHIVE=$archive; $env:NATIVE_DIR=Join-Path $inputs 'native'; $env:OUT=$stage
    Invoke-LoggedNativeTool -FilePath 'make' -LogPath (Join-Path $roleEvidence 'control-tools.log') -Arguments @(
        '-C',$source,'prepare-setup') | Out-Host
    $env:SETUP_STAGING=$stage; $env:WIX_ARCHIVE=$wix; $env:OUT=$output
    $env:TOOL_LOG=Join-Path $roleEvidence 'native-tools.log'
    try {
        Invoke-LoggedNativeTool -FilePath 'make' -LogPath (Join-Path $roleEvidence 'control-tools.log') -Arguments @(
            '-C',$source,'build-setup-msi') | Out-Host
    } finally {
        foreach ($name in @('actual.wxs','msi-inspection.json','setup-input.json')) {
            $path=Join-Path $output $name
            if (Test-Path -LiteralPath $path -PathType Leaf) { Copy-Item -LiteralPath $path -Destination $roleEvidence }
        }
        Copy-Item -LiteralPath (Join-Path $stage 'payload.wxs') -Destination $roleEvidence
    }
    $final=Get-Content -Raw -LiteralPath (Join-Path $output 'setup-input.json') | ConvertFrom-Json
    if ($final.phase -cne 'authored-msi' -or $final.build_profile -cne $profile -or
        $final.source_revision -cne $env:REVISION -or $final.version -cne $version) { throw 'Authored media identity differs.' }
    $destination=Join-Path $mediaRoot $role
    New-Item -ItemType Directory -Path $destination | Out-Null
    foreach ($name in @('setup.exe','bridge.msi','setup-input.json','msi-inspection.json','bridge.msi.sha256','setup.exe.sha256')) {
        Copy-Item -LiteralPath (Join-Path $output $name) -Destination $destination
    }
    $payload=@{}
    $actual=[xml](Get-Content -Raw -LiteralPath (Join-Path $output 'actual.wxs'))
    $componentGuids=@{}
    foreach ($component in $actual.SelectNodes("//*[local-name()='Component']")) {
        if ($componentGuids.ContainsKey($component.Id) -or
            $component.Guid -cnotmatch '^\{[0-9A-F]{8}(?:-[0-9A-F]{4}){3}-[0-9A-F]{12}\}$') {
            throw 'Actual component identity differs.'
        }
        $componentGuids[$component.Id]=[string]$component.Guid
    }
    if ($componentGuids.Count -ne 8) { throw 'Actual component count differs.' }
    foreach ($name in $payloadNames) {
        $payload[$name]=(Get-FileHash -LiteralPath (Join-Path $stage $name) -Algorithm SHA256).Hash.ToLowerInvariant()
    }
    $media+=@{
        role=$role; source_revision=$env:REVISION; version=$version; msi_version=$final.msi_version
        product=$final.product; package=$final.package; msi=(Join-Path $destination 'bridge.msi')
        msi_sha256=$final.msi_sha256; setup=(Join-Path $destination 'setup.exe'); setup_sha256=$final.setup_sha256
        actions_sha256=$final.actions_sha256; archive=$archive
        archive_sha256=(Get-FileHash -LiteralPath $archive -Algorithm SHA256).Hash.ToLowerInvariant()
        build_profile=$profile; payload_sha256=$payload; component_guids=$componentGuids
    }
}
if (@($media.setup_sha256 | Sort-Object -Unique).Count -ne 1 -or
    @($media.actions_sha256 | Sort-Object -Unique).Count -ne 1 -or
    @($media.package | Sort-Object -Unique).Count -ne 3 -or
    $media[0].product -ceq $media[1].product -or $media[1].product -cne $media[2].product -or
    $media[1].archive_sha256 -cne $media[2].archive_sha256) { throw 'Same-core real-version/private-media contract failed.' }
@{
    schema=1; source_revision=$env:REVISION; workflow_revision=$env:WORKFLOW_REVISION
    run_id=$env:GITHUB_RUN_ID; run_attempt=$env:GITHUB_RUN_ATTEMPT
    input_artifact_id=$env:NATIVE_ARTIFACT_ID; input_artifact_digest=$env:NATIVE_ARTIFACT_DIGEST
    media=$media; scope='PRIVATE DISPOSABLE LIFECYCLE MEDIA; two actual versions; no release/tags'
} | ConvertTo-Json -Depth 8 | Set-Content -Encoding utf8 -LiteralPath (Join-Path $evidence 'media-plan.json')
Copy-Item -LiteralPath (Join-Path $inputs 'provenance.json') -Destination (Join-Path $evidence 'input-provenance.json')
$head=& git -C $source rev-parse HEAD
if ($LASTEXITCODE -ne 0 -or $head -cne $env:REVISION) { throw 'Final source identity failed.' }
$state=& git -C $source status --porcelain --untracked-files=all --ignored=matching
if ($LASTEXITCODE -ne 0 -or $state) { throw 'Final source status failed or dirty.' }

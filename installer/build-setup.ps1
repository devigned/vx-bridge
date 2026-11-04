param(
    [Parameter(Mandatory=$true)][string]$Staging,
    [Parameter(Mandatory=$true)][string]$Out,
    [Parameter(Mandatory=$true)][string]$WixArchive,
    [Parameter(Mandatory=$true)][string]$ToolLog,
    [ValidateSet('normal','private-lifecycle-fault')][string]$Profile='normal'
)
Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
if ($env:OS -ne 'Windows_NT') { throw 'Windows BUILD machine only; this does not install or launch anything.' }
. (Join-Path $PSScriptRoot 'native-tools.ps1')
$root = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$stage = (Resolve-Path -LiteralPath $Staging).Path
$output = [IO.Path]::GetFullPath($Out)
if (-not [IO.Path]::IsPathRooted($Out) -or (Test-Path -LiteralPath $output) -or
    $output.StartsWith($root, [StringComparison]::OrdinalIgnoreCase) -or
    -not (Test-Path -LiteralPath (Split-Path -Parent $output) -PathType Container)) { throw 'New absolute external OUT required.' }
$pin = Get-Content -Raw -LiteralPath (Join-Path $PSScriptRoot 'toolchain.json') | ConvertFrom-Json
if ($pin.wix_version -cne '3.14.1' -or
    (Get-FileHash -LiteralPath $WixArchive -Algorithm SHA256).Hash.ToLowerInvariant() -cne $pin.wix_archive_sha256) {
    throw 'Require the exact approved official WiX3.14.1 archive.'
}
if ((Get-ItemPropertyValue 'HKLM:\SOFTWARE\Microsoft\NET Framework Setup\NDP\v4\Full' -Name Release) -lt 378389) {
    throw 'Compatible Full Framework required on builder only; no SDK/runtime installation performed.'
}
$preparedInput = Join-Path $stage 'setup-input.json'
$input = Get-Content -Raw -LiteralPath $preparedInput | ConvertFrom-Json
if ($input.schema -ne 2 -or $input.phase -cne 'prepared-input' -or
    $input.build_profile -cne $Profile -or
    $input.package_code_policy -cne 'wix-generated' -or
    $input.PSObject.Properties.Name -ccontains 'package' -or
    $input.PSObject.Properties.Name -ccontains 'msi_sha256') {
    throw 'Require prepared inputs without a guessed final package identity.'
}
if ($Profile -ceq 'private-lifecycle-fault' -and
    ($env:GITHUB_ACTIONS -cne 'true' -or $env:RUNNER_ENVIRONMENT -cne 'github-hosted' -or
    $env:GITHUB_EVENT_NAME -cne 'workflow_dispatch' -or $env:GITHUB_WORKFLOW -cne 'VX Bridge disposable lifecycle' -or
    $env:GITHUB_RUN_ATTEMPT -cne '1' -or $env:GITHUB_REPOSITORY -cne 'devigned/vx-bridge' -or
    $input.version -cne 'v0.0.2' -or $input.source_revision -cne $env:REVISION -or
    $env:REVISION -cne $env:WORKFLOW_REVISION)) { throw 'Private failure authoring is lifecycle-only, never a production profile.' }
$preparedHash = (Get-FileHash -LiteralPath $preparedInput -Algorithm SHA256).Hash.ToLowerInvariant()
$omittedAdvertise=@('PublishFeatures','PublishProduct','CreateShortcuts')
$warningPolicy=@($input.warning_exceptions)
if ($warningPolicy.Count -ne 1 -or $warningPolicy[0].tool -cne 'light.exe' -or
    $warningPolicy[0].code -ne 1008 -or $warningPolicy[0].table -cne 'AdvtExecuteSequence' -or
    @($warningPolicy[0].PSObject.Properties).Count -ne 4 -or @($warningPolicy[0].actions).Count -ne 3) {
    throw 'Only the reviewed LGHT1008 advertise-omission diagnostic exception is permitted.'
}
$omissions=@{}
foreach ($action in $warningPolicy[0].actions) {
    if ($action -cnotin $omittedAdvertise -or $omissions.ContainsKey($action)) { throw 'Unexpected advertise omission policy.' }
    $omissions[$action]=$true
}
$iceExceptions=@($input.ice_exceptions)
if ($iceExceptions.Count -ne 2 -or
    (@($iceExceptions | Sort-Object -Unique) -join '|') -cne 'ICE91|ICE96') {
    throw 'Only the reviewed per-user ICE91/nonadvertisable ICE96 exceptions are permitted.'
}
Push-Location $root
try {
    $head = (& git rev-parse HEAD).Trim()
    if ($LASTEXITCODE -ne 0 -or $head -cne $input.source_revision -or
        (& git status --porcelain --untracked-files=all --ignored=matching)) { throw 'Exact committed clean source required.' }
} finally { Pop-Location }
if ((Get-FileHash -LiteralPath (Join-Path $stage 'setup.exe') -Algorithm SHA256).Hash.ToLowerInvariant() -cne $input.setup_sha256 -or
    (Get-FileHash -LiteralPath (Join-Path $stage 'setup-actions.dll') -Algorithm SHA256).Hash.ToLowerInvariant() -cne $input.actions_sha256) {
    throw 'Native inputs changed.'
}
[xml]$prepared = Get-Content -Raw -LiteralPath (Join-Path $stage 'payload.wxs')
$package = @($prepared.SelectNodes("//*[local-name()='Package']"))
$product = @($prepared.SelectNodes("//*[local-name()='Product']"))
$preparedComponents = @($prepared.SelectNodes("//*[local-name()='Component']"))
if ($package.Count -ne 1 -or $product.Count -ne 1 -or
    $package[0].HasAttribute('Id') -or
    $package[0].InstallScope -cne 'perUser' -or $package[0].InstallPrivileges -cne 'limited' -or
    $product[0].Id -cne $input.product -or $product[0].UpgradeCode -cne $input.upgrade -or
    $preparedComponents.Count -ne 8) {
    throw 'ICE91-only exception requires the exact per-user limited x64 source scope.'
}
foreach ($component in $preparedComponents) {
    if ($component.Win64 -cne 'yes') { throw 'ICE91 exception requires only x64 components.' }
}
Push-Location $root
try {
    Invoke-LoggedNativeTool -FilePath 'go' -LogPath $ToolLog -Arguments @(
        'run','-mod=readonly','./tools/msi/setup','-verify-authoring',(Join-Path $stage 'payload.wxs'),'-profile',$Profile) | Out-Host
} finally { Pop-Location }
New-Item -ItemType Directory -Path $output | Out-Null
$wix = Join-Path $output 'verified-wix-build-tools'
Expand-Archive -LiteralPath $WixArchive -DestinationPath $wix
$msi = Join-Path $output 'bridge.msi'
Push-Location $stage
try {
    Invoke-LoggedNativeTool -FilePath (Join-Path $wix 'candle.exe') -LogPath $ToolLog -Arguments @(
        '-nologo','-wx','-arch','x64','-out',(Join-Path $output 'payload.wixobj'),'payload.wxs') | Out-Host
    Invoke-LoggedNativeTool -FilePath (Join-Path $wix 'light.exe') -LogPath $ToolLog -Arguments @(
        '-nologo','-wx','-sw1008','-sice:ICE91','-sice:ICE96','-out',$msi,(Join-Path $output 'payload.wixobj')) | Out-Host
    Invoke-LoggedNativeTool -FilePath (Join-Path $wix 'dark.exe') -LogPath $ToolLog -Arguments @(
        '-nologo','-x',(Join-Path $output 'extracted'),'-o',(Join-Path $output 'actual.wxs'),$msi) | Out-Host
} finally { Pop-Location }
[xml]$actual = Get-Content -Raw -LiteralPath (Join-Path $output 'actual.wxs')
foreach ($name in @('GSPconnect.exe','vxbridge.exe','LICENSE','THIRD_PARTY_NOTICES.txt','build-manifest.json','SHA256SUMS')) {
    $nodes = @($actual.SelectNodes("//*[local-name()='File' and @Name='$name']"))
    if ($nodes.Count -ne 1) { throw "Missing or duplicate payload: $name" }
    $path = [string]$nodes[0].Source
    if (-not [IO.Path]::IsPathRooted($path)) { $path=Join-Path $output $path }
    $path = [IO.Path]::GetFullPath($path)
    if (-not $path.StartsWith($output + '\',[StringComparison]::OrdinalIgnoreCase) -or
        (Get-FileHash -LiteralPath $path -Algorithm SHA256).Hash -cne
        (Get-FileHash -LiteralPath (Join-Path $stage $name) -Algorithm SHA256).Hash) { throw "Extracted payload differs: $name" }
}
if (@($actual.SelectNodes("//*[local-name()='File']")).Count -ne 6) { throw 'Unexpected MSI files.' }
$native = @($actual.SelectNodes("//*[local-name()='Binary' and @Id='SetupActions']"))
$nativePath = if ($native.Count -eq 1) { [string]$native[0].SourceFile } else { '' }
if ($nativePath -and -not [IO.Path]::IsPathRooted($nativePath)) { $nativePath=Join-Path $output $nativePath }
if ($native.Count -ne 1 -or
    -not ([IO.Path]::GetFullPath($nativePath)).StartsWith($output + '\',[StringComparison]::OrdinalIgnoreCase) -or
    (Get-FileHash -LiteralPath $nativePath -Algorithm SHA256).Hash.ToLowerInvariant() -cne $input.actions_sha256) {
    throw 'Actual Binary stream differs.'
}
$engine = $null; $database = $null; $view = $null; $row = $null; $summary = $null
$hash = (Get-FileHash -LiteralPath $msi -Algorithm SHA256).Hash.ToLowerInvariant()
$packageCode = $null
function Field($Record,[string]$Name,[int]$Index) {
    return $Record.GetType().InvokeMember($Name,[Reflection.BindingFlags]::GetProperty,$null,$Record,@($Index))
}
try {
    $engine = New-Object -ComObject WindowsInstaller.Installer
    $database = $engine.OpenDatabase($msi,0)
    $summary = Field $database 'SummaryInformation' 0
    if ((Field $summary 'Property' 7) -cne 'x64;1033' -or (Field $summary 'Property' 15) -ne 10) {
        throw 'Actual MSI summary differs.'
    }
    $packageCode = [string](Field $summary 'Property' 9)
    $packageGuid = [Guid]::Empty
    $actualPackage = @($actual.SelectNodes("//*[local-name()='Package']"))
    if (-not [Guid]::TryParseExact($packageCode,'B',[ref]$packageGuid) -or
        $packageGuid -eq [Guid]::Empty -or $packageCode -cne $packageGuid.ToString('B').ToUpperInvariant() -or
        $packageCode -ceq $input.product -or $packageCode -ceq $input.upgrade -or
        $actualPackage.Count -ne 1 -or $actualPackage[0].HasAttribute('Id')) {
        throw 'Actual summary PackageCode must be canonical/distinct; Dark source must omit rebuild-specific identity.'
    }
    $view = $database.OpenView('SELECT `Property`,`Value` FROM `Property`'); [void]$view.Execute()
    $properties = @{}
    while ($null -ne ($row = $view.Fetch())) {
        $name = Field $row 'StringData' 1
        if ($properties.ContainsKey($name) -or $properties.Count -ge 128) { throw 'Duplicate/oversized property table.' }
        $properties[$name] = Field $row 'StringData' 2
        [void][Runtime.InteropServices.Marshal]::FinalReleaseComObject($row); $row = $null
    }
    [void]$view.Close(); [void][Runtime.InteropServices.Marshal]::FinalReleaseComObject($view); $view = $null
    foreach ($pair in @(
        @('ProductCode',$input.product),@('ProductVersion',$input.msi_version),@('UpgradeCode',$input.upgrade),
        @('VX_SOURCE_REVISION',$input.source_revision),@('VX_OWNER_SHA256',$input.setup_sha256),
        @('VX_SETUP_PROTOCOL','1'),@('REBOOT','ReallySuppress'),@('MSIRESTARTMANAGERCONTROL','Disable'),
        @('ProductName','VX Bridge'),@('Manufacturer','VX Bridge')
    )) { if ($properties[$pair[0]] -cne $pair[1]) { throw "Emitted property differs: $($pair[0])" } }
    foreach ($pair in @(@('GUI','GSPconnect.exe'),@('CONTROL','vxbridge.exe'),@('LICENSE','LICENSE'),
        @('NOTICES','THIRD_PARTY_NOTICES.txt'),@('MANIFEST','build-manifest.json'),@('SUMS','SHA256SUMS'))) {
        $expected = (Get-FileHash -LiteralPath (Join-Path $stage $pair[1]) -Algorithm SHA256).Hash.ToLowerInvariant()
        if ($properties["VX_HASH_$($pair[0])"] -cne $expected) { throw 'Actual receipt payload hash differs.' }
    }
    if ($properties.ContainsKey('ALLUSERS') -or $properties.ContainsKey('MSIINSTALLPERUSER') -or
        $properties.ContainsKey('ARPSYSTEMCOMPONENT') -or $properties['ARPNOMODIFY'] -cne '1' -or
        $properties['ARPNOREPAIR'] -cne '1') { throw 'Machine/dual/hidden/custom-maintenance scope refused.' }
    $view = $database.OpenView('SELECT `Component`,`ComponentId`,`Directory_`,`Attributes`,`KeyPath` FROM `Component`')
    [void]$view.Execute(); $components=@{}
    $ids=@('VxGUI','VxControl','VxLicense','VxNotices','VxManifest','VxSums','Shortcut','Maintenance')
    while ($null -ne ($row=$view.Fetch())) {
        $id=Field $row 'StringData' 1
        $authored=@($prepared.SelectNodes("//*[local-name()='Component' and @Id='$id']"))
        $directory=if ($id -ceq 'Shortcut') { 'ProgramMenuFolder' } else { 'INSTALLDIR' }
        if ($id -cnotin $ids -or $components.ContainsKey($id) -or $authored.Count -ne 1 -or
            (Field $row 'StringData' 2) -cne $authored[0].Guid -or
            (Field $row 'StringData' 3) -cne $directory -or (Field $row 'IntegerData' 4) -ne 260) {
            throw 'Unexpected actual component scope/attributes.'
        }
        $components[$id]=Field $row 'StringData' 5
        [void][Runtime.InteropServices.Marshal]::FinalReleaseComObject($row); $row=$null
    }
    if ($components.Count -ne 8) { throw 'Missing actual component.' }
    [void]$view.Close(); [void][Runtime.InteropServices.Marshal]::FinalReleaseComObject($view); $view=$null
    $view=$database.OpenView('SELECT `Registry`,`Root`,`Key`,`Name`,`Value`,`Component_` FROM `Registry`')
    [void]$view.Execute(); $seenRegistry=@{}
    while ($null -ne ($row=$view.Fetch())) {
        $id=Field $row 'StringData' 1; $component=Field $row 'StringData' 6
        $key=if ($component -ceq 'Maintenance') { 'Software\VxInfiniteTeesBridge\Installer' } else { 'Software\VxInfiniteTeesBridge\MSI' }
        $name=if ($component -ceq 'Maintenance') { 'SourcePackage' } else { $component }
        $value=if ($component -ceq 'Maintenance') { '[VX_CACHE_SOURCE]' } elseif ($component -ceq 'Shortcut') { '#1' } else { $input.source_revision }
        if (-not $components.ContainsKey($component) -or $seenRegistry.ContainsKey($component) -or
            $components[$component] -cne $id -or (Field $row 'IntegerData' 2) -ne 1 -or
            (Field $row 'StringData' 3) -cne $key -or (Field $row 'StringData' 4) -cne $name -or
            (Field $row 'StringData' 5) -cne $value) { throw 'Actual registry ownership differs.' }
        $seenRegistry[$component]=$true
        [void][Runtime.InteropServices.Marshal]::FinalReleaseComObject($row); $row=$null
    }
    if ($seenRegistry.Count -ne 8) { throw 'Missing actual registry key path.' }
    [void]$view.Close(); [void][Runtime.InteropServices.Marshal]::FinalReleaseComObject($view); $view=$null
    $view=$database.OpenView('SELECT `File`,`Component_`,`FileName`,`FileSize`,`Sequence` FROM `File`')
    [void]$view.Execute(); $seenFiles=@{}; $sequences=@{}
    $fileNames=@{ VxGUI='GSPconnect.exe'; VxControl='vxbridge.exe'; VxLicense='LICENSE'
        VxNotices='THIRD_PARTY_NOTICES.txt'; VxManifest='build-manifest.json'; VxSums='SHA256SUMS' }
    while ($null -ne ($row=$view.Fetch())) {
        $id=Field $row 'StringData' 1; $name=(Field $row 'StringData' 3).Split('|')[-1]
        $sequence=Field $row 'IntegerData' 5
        if (-not $fileNames.ContainsKey($id) -or $seenFiles.ContainsKey($id) -or
            (Field $row 'StringData' 2) -cne $id -or $name -cne $fileNames[$id] -or
            (Field $row 'IntegerData' 4) -ne (Get-Item -LiteralPath (Join-Path $stage $name)).Length -or
            $sequence -lt 1 -or $sequence -gt 6 -or $sequences.ContainsKey($sequence)) {
            throw 'Actual File/CAB mapping differs.'
        }
        $seenFiles[$id]=$true; $sequences[$sequence]=$true
        [void][Runtime.InteropServices.Marshal]::FinalReleaseComObject($row); $row=$null
    }
    if ($seenFiles.Count -ne 6) { throw 'Missing actual file row.' }
    [void]$view.Close(); [void][Runtime.InteropServices.Marshal]::FinalReleaseComObject($view); $view=$null
    $view=$database.OpenView('SELECT `DiskId`,`LastSequence`,`Cabinet` FROM `Media`')
    [void]$view.Execute(); $row=$view.Fetch()
    if ($null -eq $row -or (Field $row 'IntegerData' 1) -ne 1 -or (Field $row 'IntegerData' 2) -ne 6 -or
        -not (Field $row 'StringData' 3).StartsWith('#')) { throw 'Actual media must be one embedded six-file CAB.' }
    [void][Runtime.InteropServices.Marshal]::FinalReleaseComObject($row); $row=$view.Fetch()
    if ($null -ne $row) { throw 'Unexpected extra media.' }
    [void]$view.Close(); [void][Runtime.InteropServices.Marshal]::FinalReleaseComObject($view); $view=$null
    $view = $database.OpenView('SELECT `Action`,`Type`,`Source`,`Target` FROM `CustomAction`'); [void]$view.Execute()
    $types = @{ VxSetupAuthorize=1; VxSetupUndoStartup=1281; VxSetupStartup=1025; VxSetupPreFinalize=1; VxSetLocation=51;
        VxRemoveUndo=1281; VxRemoveBegin=1025; VxRemoveFinish=1025; VxSetRoot=51 }
    if ($Profile -ceq 'private-lifecycle-fault') { $types['VxLifecycleExpectedFail']=19 }
    $seen = @{}
    while ($null -ne ($row = $view.Fetch())) {
        $name = Field $row 'StringData' 1
        $source = if ($name -ceq 'VxSetLocation') { 'ARPINSTALLLOCATION' } elseif ($name -ceq 'VxSetRoot') { 'INSTALLDIR' } elseif ($name -ceq 'VxLifecycleExpectedFail') { '' } else { 'SetupActions' }
        $target = if ($name -ceq 'VxSetLocation') { '[INSTALLDIR]' } elseif ($name -ceq 'VxSetRoot') {
            '[LocalAppDataFolder]Programs\VxInfiniteTeesBridge\'
        } elseif ($name -ceq 'VxLifecycleExpectedFail') { 'PRIVATE_LIFECYCLE_EXPECTED_POST_EXECUTE_FAILURE' } else { $name }
        if (-not $types.ContainsKey($name) -or $seen.ContainsKey($name) -or
            (Field $row 'IntegerData' 2) -ne $types[$name] -or
            (Field $row 'StringData' 3) -cne $source -or (Field $row 'StringData' 4) -cne $target) {
            throw 'Unexpected actual custom action.'
        }
        $seen[$name]=$true
        [void][Runtime.InteropServices.Marshal]::FinalReleaseComObject($row); $row=$null
    }
    if ($seen.Count -ne $types.Count) { throw 'Missing actual custom action.' }
    [void]$view.Close(); [void][Runtime.InteropServices.Marshal]::FinalReleaseComObject($view); $view=$null
    $view = $database.OpenView('SELECT `Action`,`Condition`,`Sequence` FROM `InstallExecuteSequence`'); [void]$view.Execute()
    $sequence=@{}; $conditions=@{}
    while ($null -ne ($row=$view.Fetch())) {
        $action=Field $row 'StringData' 1
        $conditions[$action]=Field $row 'StringData' 2
        $sequence[$action] = Field $row 'IntegerData' 3
        [void][Runtime.InteropServices.Marshal]::FinalReleaseComObject($row); $row=$null
    }
    foreach ($action in @('VxSetRoot','CostFinalize','VxSetupAuthorize','InstallValidate','InstallInitialize','VxSetupUndoStartup','VxSetupStartup',
        'VxRemoveUndo','VxRemoveBegin','RemoveRegistryValues','RemoveFiles','VxRemoveFinish',
        'FindRelatedProducts','RemoveExistingProducts','InstallFiles','InstallExecute','VxSetupPreFinalize','InstallFinalize',
        'CreateShortcuts','PublishFeatures','PublishProduct')) {
        if (-not $sequence.ContainsKey($action)) { throw "Missing actual sequence: $action" }
    }
    foreach ($action in @('CreateShortcuts','PublishFeatures','PublishProduct')) {
        if ($sequence[$action] -le 0 -or $conditions[$action] -ceq '0') {
            throw 'Normal install publication/shortcuts must remain enabled.'
        }
    }
    if ($conditions.RemoveExistingProducts -cne 'NOT REMOVE AND WIX_UPGRADE_DETECTED' -or
        $sequence.FindRelatedProducts -ge $sequence.VxSetupAuthorize -or
        $sequence.RemoveExistingProducts -le $sequence.InstallFinalize -or
        $sequence.VxSetRoot -ge $sequence.CostFinalize -or $conditions.VxSetRoot -cne '1' -or
        $sequence.VxSetupAuthorize -ge $sequence.InstallValidate -or $sequence.VxSetupUndoStartup -le $sequence.InstallInitialize -or
        $sequence.VxSetupStartup -le $sequence.VxSetupUndoStartup -or $sequence.VxSetupStartup -ge $sequence.InstallFiles -or
        $sequence.VxSetupPreFinalize -le $sequence.InstallExecute -or $sequence.VxSetupPreFinalize -ge $sequence.InstallFinalize -or
        $sequence.VxRemoveUndo -le $sequence.InstallInitialize -or $sequence.VxRemoveBegin -le $sequence.VxRemoveUndo -or
        $sequence.VxRemoveBegin -ge $sequence.RemoveRegistryValues -or
        $sequence.VxRemoveFinish -le $sequence.RemoveFiles -or $sequence.VxRemoveFinish -ge $sequence.InstallExecute) {
        throw 'Unsafe actual MSI sequence.'
    }
    foreach ($action in @('VxRemoveUndo','VxRemoveBegin','VxRemoveFinish')) {
        if ($conditions[$action] -cne 'VX_REMOVE_MODE = "1"') { throw 'Removal-only callback condition changed.' }
    }
    foreach ($action in @('VxSetupUndoStartup','VxSetupStartup')) {
        if ($conditions[$action] -cne 'NOT VX_REMOVE_MODE AND NOT UPGRADINGPRODUCTCODE AND NOT VX_PRESERVE_MODE') { throw 'Install/repair startup callback condition changed.' }
    }
    if ($Profile -ceq 'private-lifecycle-fault') {
        if ($sequence.VxLifecycleExpectedFail -ne 6550 -or
            $sequence.VxLifecycleExpectedFail -le $sequence.InstallExecute -or
            $sequence.VxLifecycleExpectedFail -ge $sequence.InstallFinalize -or
            $conditions.VxLifecycleExpectedFail -cne 'NOT REMOVE AND VX_LIFECYCLE_FAIL = "1"') {
            throw 'Private deliberate failure must follow the actual mutation script and exclude uninstall.'
        }
    } elseif ($sequence.ContainsKey('VxLifecycleExpectedFail')) { throw 'Private failure entered normal production sequence.' }
    if (@($actual.SelectNodes("//*[local-name()='RegistryValue' and @Name='UninstallString']")).Count -ne 0) {
        throw 'Custom ARP uninstall owner reintroduced.'
    }
    [void]$view.Close(); [void][Runtime.InteropServices.Marshal]::FinalReleaseComObject($view); $view=$null
    $view=$database.OpenView('SELECT `Name` FROM `_Tables`'); [void]$view.Execute(); $tables=@{}
    while ($null -ne ($row=$view.Fetch())) {
        $tables[(Field $row 'StringData' 1)]=$true
        [void][Runtime.InteropServices.Marshal]::FinalReleaseComObject($row); $row=$null
    }
    [void]$view.Close(); [void][Runtime.InteropServices.Marshal]::FinalReleaseComObject($view); $view=$null
    foreach ($table in @('AppSearch','RegLocator')) {
        if ($tables.ContainsKey($table)) {
            $query=if ($table -ceq 'AppSearch') { 'SELECT `Property`,`Signature_` FROM `AppSearch`' } else {
                'SELECT `Signature_`,`Root`,`Key`,`Name`,`Type` FROM `RegLocator`'
            }
            $view=$database.OpenView($query); [void]$view.Execute(); $row=$view.Fetch()
            if ($Profile -ceq 'normal') {
                if ($null -ne $row) { throw 'Normal production profile cannot have private lifecycle searches.' }
            } else {
                if ($null -eq $row) { throw 'Missing private account-hive failure search.' }
                if ($table -ceq 'AppSearch') {
                    if ((Field $row 'StringData' 1) -cne 'VX_LIFECYCLE_FAIL' -or
                        (Field $row 'StringData' 2) -cne 'VxLifecycleFault' -or
                        -not $sequence.ContainsKey('AppSearch') -or $sequence.AppSearch -le 0 -or
                        $sequence.AppSearch -ge $sequence.CostFinalize) { throw 'Private search property/execute phase differs.' }
                } elseif ((Field $row 'StringData' 1) -cne 'VxLifecycleFault' -or
                    (Field $row 'IntegerData' 2) -ne 1 -or
                    (Field $row 'StringData' 3) -cne 'Software\VXBridgeLifecycleHarness' -or
                    (Field $row 'StringData' 4) -cne 'FailUpgrade' -or
                    (Field $row 'IntegerData' 5) -ne 18) { throw 'Private search must use the exact test-account HKCU64 string fixture.' }
                [void][Runtime.InteropServices.Marshal]::FinalReleaseComObject($row); $row=$view.Fetch()
                if ($null -ne $row) { throw 'Unexpected extra private search.' }
            }
            [void]$view.Close(); [void][Runtime.InteropServices.Marshal]::FinalReleaseComObject($view); $view=$null
        } elseif ($Profile -ceq 'private-lifecycle-fault') { throw 'Missing private search table.' }
    }
    $view=$database.OpenView('SELECT `Action`,`Condition`,`Sequence` FROM `InstallUISequence`')
    [void]$view.Execute(); $uiSequence=@{}; $uiConditions=@{}
    while ($null -ne ($row=$view.Fetch())) {
        $action=Field $row 'StringData' 1
        $uiSequence[$action]=Field $row 'IntegerData' 3; $uiConditions[$action]=Field $row 'StringData' 2
        [void][Runtime.InteropServices.Marshal]::FinalReleaseComObject($row); $row=$null
    }
    if (-not $uiSequence.ContainsKey('VxSetRoot') -or -not $uiSequence.ContainsKey('CostFinalize') -or
        $uiSequence.VxSetRoot -ge $uiSequence.CostFinalize -or $uiConditions.VxSetRoot -cne '1') {
        throw 'Fixed target must also be initialized before full-UI costing.'
    }
    [void]$view.Close(); [void][Runtime.InteropServices.Marshal]::FinalReleaseComObject($view); $view=$null
    $view=$database.OpenView('SELECT `Directory`,`Directory_Parent`,`DefaultDir` FROM `Directory`')
    [void]$view.Execute(); $directories=@{}
    while ($null -ne ($row=$view.Fetch())) {
        $id=Field $row 'StringData' 1
        if ($directories.ContainsKey($id) -or $id -ceq 'ProgramsFolder') { throw 'Shared Programs must not be an MSI-owned directory.' }
        $directories[$id]=@{ parent=(Field $row 'StringData' 2); name=(Field $row 'StringData' 3) }
        [void][Runtime.InteropServices.Marshal]::FinalReleaseComObject($row); $row=$null
    }
    if (-not $directories.ContainsKey('INSTALLDIR') -or $directories.INSTALLDIR.parent -cne 'LocalAppDataFolder' -or
        $directories.INSTALLDIR.name.Split('|')[-1] -cne 'VxInfiniteTeesBridge') { throw 'Actual fixed-root directory topology differs.' }
    [void]$view.Close(); [void][Runtime.InteropServices.Marshal]::FinalReleaseComObject($view); $view=$null
    $view=$database.OpenView('SELECT `Action`,`Condition`,`Sequence` FROM `AdvtExecuteSequence`')
    [void]$view.Execute(); $advertiseSequence=@{}
    $nonmutatingAdvertise=@('CostInitialize','CostFinalize','InstallValidate','InstallInitialize','InstallFinalize')
    while ($null -ne ($row=$view.Fetch())) {
        $action=Field $row 'StringData' 1; $condition=Field $row 'StringData' 2
        $position=Field $row 'IntegerData' 3
        if ($advertiseSequence.ContainsKey($action) -or
            $action -cnotin $nonmutatingAdvertise -or $position -le 0 -or $condition) {
            throw 'Actual advertising sequence must not publish or create shortcuts.'
        }
        $advertiseSequence[$action]=@{ condition=$condition; sequence=$position }
        [void][Runtime.InteropServices.Marshal]::FinalReleaseComObject($row); $row=$null
    }
    foreach ($action in $omittedAdvertise) {
        if ($advertiseSequence.ContainsKey($action)) { throw 'Intentionally omitted advertise action was emitted.' }
    }
    [void]$view.Close(); [void][Runtime.InteropServices.Marshal]::FinalReleaseComObject($view); $view=$null
    $view=$database.OpenView('SELECT `Feature`,`Attributes` FROM `Feature`'); [void]$view.Execute()
    $row=$view.Fetch()
    if ($null -eq $row -or (Field $row 'StringData' 1) -cne 'Bridge' -or
        ((Field $row 'IntegerData' 2) -band 44) -ne 8) { throw 'Actual feature must prohibit advertising.' }
    [void][Runtime.InteropServices.Marshal]::FinalReleaseComObject($row); $row=$view.Fetch()
    if ($null -ne $row) { throw 'Unexpected extra feature.' }
    [void]$view.Close(); [void][Runtime.InteropServices.Marshal]::FinalReleaseComObject($view); $view=$null
    foreach ($table in @('Class','Extension','MIME','ProgId','PublishComponent','MsiAssembly')) {
        if ($tables.ContainsKey($table)) {
            $view=$database.OpenView("SELECT * FROM ``$table``"); [void]$view.Execute(); $row=$view.Fetch()
            if ($null -ne $row) { throw 'Unexpected registration/advertised resource table data.' }
            [void]$view.Close(); [void][Runtime.InteropServices.Marshal]::FinalReleaseComObject($view); $view=$null
        }
    }
    $view=$database.OpenView('SELECT `Shortcut`,`Directory_`,`Component_`,`Target`,`Arguments` FROM `Shortcut`')
    [void]$view.Execute(); $shortcuts=@{}
    while ($null -ne ($row=$view.Fetch())) {
        $id=Field $row 'StringData' 1
        $component=if ($id -ceq 'SetupShortcut') { 'Maintenance' } else { 'Shortcut' }
        $target=if ($id -ceq 'BridgeShortcut') { '[INSTALLDIR]GSPconnect.exe' } elseif ($id -ceq 'DiagnosticsShortcut') { '[INSTALLDIR]vxbridge.exe' } else { '[VX_MAINTENANCE_EXE]' }
        $arguments=if ($id -ceq 'BridgeShortcut') { '' } elseif ($id -ceq 'DiagnosticsShortcut') { '--diagnostics-dialog' } else { '/maintain' }
        if ($id -cnotin @('BridgeShortcut','SetupShortcut','DiagnosticsShortcut') -or $shortcuts.ContainsKey($id) -or
            (Field $row 'StringData' 2) -cne 'ProgramMenuFolder' -or
            (Field $row 'StringData' 3) -cne $component -or (Field $row 'StringData' 4) -cne $target -or
            (Field $row 'StringData' 5) -cne $arguments) { throw 'Unexpected advertised or foreign shortcut.' }
        $shortcuts[$id]=$target
        [void][Runtime.InteropServices.Marshal]::FinalReleaseComObject($row); $row=$null
    }
    if ($shortcuts.Count -ne 3) { throw 'Missing actual nonadvertised shortcut.' }
    [void]$view.Close(); [void][Runtime.InteropServices.Marshal]::FinalReleaseComObject($view); $view=$null
    $view=$database.OpenView('SELECT `FileKey`,`Component_`,`FileName`,`DirProperty`,`InstallMode` FROM `RemoveFile`'); [void]$view.Execute()
    $row=$view.Fetch()
    if ($null -eq $row -or (Field $row 'StringData' 1) -cne 'VxRemoveOwnedRoot' -or
        (Field $row 'StringData' 2) -cne 'Maintenance' -or (Field $row 'StringData' 3) -cne '' -or
        (Field $row 'StringData' 4) -cne 'INSTALLDIR' -or (Field $row 'IntegerData' 5) -ne 2) {
        throw 'Require only the exact owned-root uninstall directory row.'
    }
    [void][Runtime.InteropServices.Marshal]::FinalReleaseComObject($row); $row=$view.Fetch()
    if ($null -ne $row) { throw 'Other cleanup rows must come from actual-session literal owned inventory.' }
    @{ schema=2; build_profile=$Profile; source_revision=$head; package=$packageCode; msi_sha256=$hash; prepared_input_sha256=$preparedHash;
        properties=$properties; components=$components; files=$fileNames;
        sequence=$sequence; conditions=$conditions; advertise_sequence=$advertiseSequence; shortcuts=$shortcuts;
        directories=$directories; ui_sequence=$uiSequence; root_cleanup='VxRemoveOwnedRoot/Maintenance/INSTALLDIR/uninstall';
        advertise_omissions=$omittedAdvertise; warning_exceptions=$warningPolicy;
        ice_exceptions=$iceExceptions;
        windows_execution='authoring/static only; no installation' } |
        ConvertTo-Json -Depth 5 | Set-Content -Encoding utf8 -LiteralPath (Join-Path $output 'msi-inspection.json')
} finally {
    foreach ($object in @($row,$view,$summary,$database,$engine)) {
        if ($null -ne $object) { [void][Runtime.InteropServices.Marshal]::FinalReleaseComObject($object) }
    }
}
Copy-Item -LiteralPath (Join-Path $stage 'setup.exe') -Destination (Join-Path $output 'setup.exe')
if ((Get-FileHash -LiteralPath $msi -Algorithm SHA256).Hash.ToLowerInvariant() -cne $hash -or
    (Get-FileHash -LiteralPath $preparedInput -Algorithm SHA256).Hash.ToLowerInvariant() -cne $preparedHash) {
    throw 'Verified package/input bytes changed during inspection.'
}
$final = [ordered]@{}
foreach ($property in $input.PSObject.Properties) { $final[$property.Name]=$property.Value }
$final['phase']='authored-msi'
$final['package']=$packageCode
$final['msi_sha256']=$hash
$final['prepared_input_sha256']=$preparedHash
$final['scope']='BUILD ONLY: authored and statically inspected; install/repair/uninstall behavior unrun; no release activation'
$final | ConvertTo-Json -Depth 5 | Set-Content -Encoding utf8 -LiteralPath (Join-Path $output 'setup-input.json')
"$hash  bridge.msi" | Set-Content -Encoding ascii -LiteralPath (Join-Path $output 'bridge.msi.sha256')
Copy-Item -LiteralPath (Join-Path $PSScriptRoot 'NOTICE.txt') -Destination (Join-Path $output 'INSTALLER-NOTICE.txt')
"$($input.setup_sha256)  setup.exe" | Set-Content -Encoding ascii -LiteralPath (Join-Path $output 'setup.exe.sha256')

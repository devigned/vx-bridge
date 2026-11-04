param(
    [Parameter(Mandatory=$true)][ValidateSet('Enable','Restore')][string]$Mode,
    [string]$TestSID
)
Set-StrictMode -Version Latest
$ErrorActionPreference='Stop'
if ($env:OS -cne 'Windows_NT' -or $env:RUNNER_ENVIRONMENT -cne 'github-hosted' -or
    $env:GITHUB_ACTIONS -cne 'true' -or $env:GITHUB_EVENT_NAME -cne 'workflow_dispatch' -or
    $env:GITHUB_WORKFLOW -cne 'VX Bridge disposable lifecycle' -or
    $env:GITHUB_REPOSITORY -cne 'devigned/vx-bridge' -or
    $env:GITHUB_RUN_ATTEMPT -cne '1' -or $env:REVISION -cnotmatch '^[0-9a-f]{40}$' -or
    $env:REVISION -cne $env:WORKFLOW_REVISION) { throw 'Policy adjustment is disposable manual lifecycle only.' }
$identity=[Security.Principal.WindowsIdentity]::GetCurrent()
if (-not [Security.Principal.WindowsPrincipal]::new($identity).IsInRole(
    [Security.Principal.WindowsBuiltInRole]::Administrator)) { throw 'Only the hosted admin harness manages this prerequisite.' }
$policyPath='Software\Policies\Microsoft\Windows\Installer'
$recordPath=Join-Path $env:LIFECYCLE_EVIDENCE 'hosted-msi-policy.json'
$base=[Microsoft.Win32.RegistryKey]::OpenBaseKey(
    [Microsoft.Win32.RegistryHive]::LocalMachine,[Microsoft.Win32.RegistryView]::Registry64)
$record=$null
$ownedRecord=$false
function Read-MachinePolicy {
    $key=$base.OpenSubKey($policyPath,$false)
    if ($null -eq $key) { return [ordered]@{key_exists=$false;entries=@();subkeys=@()} }
    try {
        $values=@(foreach ($name in @($key.GetValueNames() | Sort-Object -CaseSensitive)) {
            $kind=$key.GetValueKind($name)
            if ($kind -eq [Microsoft.Win32.RegistryValueKind]::Unknown) { throw 'Unknown machine policy value type.' }
            $data=$key.GetValue($name,$null,[Microsoft.Win32.RegistryValueOptions]::DoNotExpandEnvironmentNames)
            [ordered]@{name=$name;kind=$kind.ToString();data=$data}
        })
        return [ordered]@{key_exists=$true;entries=$values;subkeys=@($key.GetSubKeyNames() | Sort-Object -CaseSensitive)}
    } finally { $key.Dispose() }
}
function Find-Value($Snapshot,[string]$Name) {
    $matches=@($Snapshot.entries | Where-Object { $_.name -ieq $Name })
    if ($matches.Count -gt 1) { throw 'Ambiguous policy value.' }
    if ($matches.Count) { return $matches[0] }
    return $null
}
function Check-DisableValue($Value) {
    if ($null -ne $Value -and ($Value.kind -cne 'DWord' -or [long]$Value.data -notin @(0,1,2))) {
        throw 'Unknown DisableMSI type/value; never guess or change another policy.'
    }
}
function Check-NoElevation($Snapshot) {
    $value=Find-Value $Snapshot 'AlwaysInstallElevated'
    if ($null -ne $value -and ($value.kind -cne 'DWord' -or [long]$value.data -ne 0)) {
        throw 'Machine AlwaysInstallElevated must remain effectively0.'
    }
}
function Signature($Value) { return ConvertTo-Json -InputObject $Value -Depth 20 -Compress }
function Unrelated-Signature($Snapshot) {
    return Signature ([ordered]@{entries=@($Snapshot.entries | Where-Object { $_.name -ine 'DisableMSI' });subkeys=@($Snapshot.subkeys)})
}
function Save-Record {
    $record.updated_utc=[DateTime]::UtcNow.ToString('o')
    $temporary=$recordPath+'.next'
    [IO.File]::WriteAllText($temporary,($record | ConvertTo-Json -Depth 25),[Text.UTF8Encoding]::new($false))
    [IO.File]::Move($temporary,$recordPath,$true)
}
try {
    if ($Mode -ceq 'Enable') {
        if (Test-Path -LiteralPath $recordPath) { throw 'Never reuse a policy adjustment record.' }
        if ($TestSID -cnotmatch '^S-1-5-21-(?:[0-9]+-){3}[0-9]+$' -or
            $TestSID -ceq $identity.User.Value) {
            throw 'Exact independently created standard SID required.'
        }
        $parent=$base.OpenSubKey('Software\Policies\Microsoft\Windows',$false)
        if ($null -eq $parent) { throw 'Existing policy parent required; never create unknown policy ancestors.' }
        $parent.Dispose()
        $before=Read-MachinePolicy
        $original=Find-Value $before 'DisableMSI'
        Check-DisableValue $original
        Check-NoElevation $before
        $raw=if ($null -ne $original) {
            [Convert]::ToHexString([BitConverter]::GetBytes([int]$original.data))
        } else { $null }
        $record=[ordered]@{
            schema=1; source_revision=$env:REVISION; workflow_revision=$env:WORKFLOW_REVISION
            run_id=$env:GITHUB_RUN_ID; run_attempt=$env:GITHUB_RUN_ATTEMPT
            harness_sid=$identity.User.Value; test_user_sid=$TestSID
            environment='policy-adjusted disposable GitHub-hosted Windows; not stock policy-locked-host compatibility'
            path='HKLM64\Software\Policies\Microsoft\Windows\Installer'; value='DisableMSI'
            before=$before; original_disable_msi_raw_hex=$raw; active=$null; restored=$null
            status='prepared'; failure=$null; failures=@(); updated_utc=$null
        }
        $ownedRecord=$true
        Save-Record
        $key=$base.CreateSubKey($policyPath,$true)
        try {
            $name=if ($null -ne $original) { $original.name } else { 'DisableMSI' }
            $key.SetValue($name,0,[Microsoft.Win32.RegistryValueKind]::DWord)
            $key.Flush()
        } finally { $key.Dispose() }
        $active=Read-MachinePolicy
        $enabled=Find-Value $active 'DisableMSI'
        if ($null -eq $enabled -or $enabled.kind -cne 'DWord' -or [long]$enabled.data -ne 0 -or
            (Unrelated-Signature $active) -cne (Unrelated-Signature $before)) { throw 'Exact active policy/unchanged unrelated contents not proved.' }
        Check-NoElevation $active
        $record.active=$active; $record.status='active'
        Save-Record
    } else {
        if (-not (Test-Path -LiteralPath $recordPath)) {
            Write-Output 'No policy adjustment began; nothing to restore.'
            return
        }
        $record=Get-Content -Raw -LiteralPath $recordPath | ConvertFrom-Json -AsHashtable
        if ($record.schema -ne 1 -or $record.source_revision -cne $env:REVISION -or
            $record.workflow_revision -cne $env:WORKFLOW_REVISION -or $record.run_id -cne $env:GITHUB_RUN_ID -or
            $record.run_attempt -cne '1' -or $record.harness_sid -cne $identity.User.Value -or
            $record.path -cne 'HKLM64\Software\Policies\Microsoft\Windows\Installer' -or $record.value -cne 'DisableMSI' -or
            $record.status -cnotin @('prepared','active','restored','restoration-failed')) { throw 'Unknown policy restoration ownership/state.' }
        $ownedRecord=$true
        $before=$record.before
        $original=Find-Value $before 'DisableMSI'
        Check-DisableValue $original
        if ($null -ne $original -and $record.original_disable_msi_raw_hex -cne
            [Convert]::ToHexString([BitConverter]::GetBytes([int]$original.data))) { throw 'Original DWORD bytes differ.' }
        $live=Read-MachinePolicy
        $current=Find-Value $live 'DisableMSI'
        Check-DisableValue $current
        $isActive=$null -ne $current -and $current.kind -ceq 'DWord' -and [long]$current.data -eq 0
        if (-not $isActive -and (Signature $current) -cne (Signature $original)) { throw 'Unknown concurrent policy write; restoration refused.' }
        if ($isActive) {
            $key=$base.OpenSubKey($policyPath,$true)
            if ($null -eq $key) { throw 'Active policy key disappeared.' }
            try {
                if ($null -eq $original) { $key.DeleteValue('DisableMSI',$false) }
                else { $key.SetValue($original.name,[int]$original.data,[Microsoft.Win32.RegistryValueKind]::DWord) }
                $key.Flush()
            } finally { $key.Dispose() }
        }
        $after=Read-MachinePolicy
        if (-not $before.key_exists -and $after.key_exists -and @($after.entries).Count -eq 0 -and @($after.subkeys).Count -eq 0) {
            # Only the exact newly created, checked-empty leaf; never a shared subtree.
            $parent=$base.OpenSubKey('Software\Policies\Microsoft\Windows',$true)
            try { $parent.DeleteSubKey('Installer',$false) } finally { $parent.Dispose() }
            $after=Read-MachinePolicy
        }
        $record.restored=$after
        if ((Signature $after) -cne (Signature $before)) { throw 'Policy restoration differs from exact original key/value/unrelated contents.' }
        Check-NoElevation $after
        $record.status='restored'; $record.failure=$null
        Save-Record
    }
} catch {
    if ($ownedRecord) {
        $record.failure=$_.Exception.Message
        $record.failures+=@([ordered]@{mode=$Mode;utc=[DateTime]::UtcNow.ToString('o');message=$_.Exception.Message})
        if ($Mode -ceq 'Restore') { $record.status='restoration-failed' }
        Save-Record
    }
    throw
} finally { $base.Dispose() }

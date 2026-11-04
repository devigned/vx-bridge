Set-StrictMode -Version Latest
$ErrorActionPreference='Stop'
if ($env:OS -cne 'Windows_NT' -or $env:RUNNER_ENVIRONMENT -cne 'github-hosted' -or
    $env:GITHUB_EVENT_NAME -cne 'workflow_dispatch' -or $env:GITHUB_WORKFLOW -cne 'VX Bridge disposable lifecycle' -or
    $env:GITHUB_REPOSITORY -cne 'devigned/vx-bridge' -or
    $env:GITHUB_RUN_ATTEMPT -cne '1' -or $env:REVISION -cne $env:WORKFLOW_REVISION) {
    throw 'Product execution is limited to the exact first-attempt manual disposable lifecycle workflow.'
}
$evidence=(Resolve-Path -LiteralPath $env:LIFECYCLE_EVIDENCE).Path
$inputs=(Resolve-Path -LiteralPath $env:BUILD_INPUT_DIR).Path
$plan=Get-Content -Raw -LiteralPath (Join-Path $evidence 'media-plan.json') | ConvertFrom-Json
if ($plan.source_revision -cne $env:REVISION -or $plan.workflow_revision -cne $env:WORKFLOW_REVISION -or
    $plan.run_id -cne $env:GITHUB_RUN_ID -or $plan.run_attempt -cne '1' -or @($plan.media).Count -ne 3) {
    throw 'Actual authored media plan differs.'
}
$harnessIdentity=[Security.Principal.WindowsIdentity]::GetCurrent()
$harnessPrincipal=[Security.Principal.WindowsPrincipal]::new($harnessIdentity)
if (-not $harnessPrincipal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) {
    throw 'Hosted admin harness is required only to create/launch the separate standard account.'
}
$evidenceACL=[Security.AccessControl.DirectorySecurity]::new()
$evidenceACL.SetOwner($harnessIdentity.User)
$evidenceACL.SetAccessRuleProtection($true,$false)
foreach ($ownerSID in @($harnessIdentity.User.Value,'S-1-5-18','S-1-5-32-544')) {
    $evidenceACL.AddAccessRule([Security.AccessControl.FileSystemAccessRule]::new(
        [Security.Principal.SecurityIdentifier]::new($ownerSID),
        [Security.AccessControl.FileSystemRights]::FullControl,
        [Security.AccessControl.InheritanceFlags]'ContainerInherit,ObjectInherit',
        [Security.AccessControl.PropagationFlags]::None,[Security.AccessControl.AccessControlType]::Allow))
}
Set-Acl -LiteralPath $evidence -AclObject $evidenceACL
$account="vxlife$env:GITHUB_RUN_ID"
if ($account.Length -gt 20 -or @(Get-LocalUser | Where-Object { $_.Name -ceq $account }).Count) {
    throw 'Fresh dedicated disposable account required; never adopt an existing account.'
}
$result=[ordered]@{
    schema=1; source_revision=$env:REVISION; workflow_revision=$env:WORKFLOW_REVISION
    run_id=$env:GITHUB_RUN_ID; run_attempt=$env:GITHUB_RUN_ATTEMPT; account=$account
    harness_sid=$harnessIdentity.User.Value; account_sid=$null; account_admin_member=$null
    worker_pid=$null; worker_exit=$null; profile=$null; process_trace_complete=$false
    implicit_product_launches=@(); success=$false; failure=$null
    cleanup_proof='Actual per-user final audit before VM/account destruction, never harness deletion.'
}
$watch=$null; $subscription="VXBridgeLifecycle-$env:GITHUB_RUN_ID"
$worker=$null
$policyScript=Join-Path $PSScriptRoot 'setup-lifecycle-policy.ps1'
$os=Get-CimInstance Win32_OperatingSystem
$engine=Join-Path $env:SystemRoot 'System32/msi.dll'
@{caption=$os.Caption;version=$os.Version;build=$os.BuildNumber;architecture=$os.OSArchitecture
    msi_file_version=(Get-Item -LiteralPath $engine).VersionInfo.FileVersion
    msi_sha256=(Get-FileHash -LiteralPath $engine -Algorithm SHA256).Hash.ToLowerInvariant()
    image_os=$env:ImageOS;image_version=$env:ImageVersion;contract='recovery-first-v2'} |
    ConvertTo-Json | Set-Content -LiteralPath (Join-Path $evidence 'actual-os-engine.json') -Encoding utf8
try {
    $random=[byte[]]::new(32)
    $rng=[Security.Cryptography.RandomNumberGenerator]::Create()
    try { $rng.GetBytes($random) } finally { $rng.Dispose() }
    $password='A!7'+[Convert]::ToBase64String($random)
    Write-Output "::add-mask::$password"
    $secure=ConvertTo-SecureString -String $password -AsPlainText -Force
    $user=New-LocalUser -Name $account -Password $secure -AccountNeverExpires -PasswordNeverExpires -UserMayNotChangePassword
    $sid=$user.SID.Value; $result.account_sid=$sid
    $usersGroup=Get-LocalGroup -SID 'S-1-5-32-545'
    $members=@(Get-LocalGroupMember -Group $usersGroup)
    if ($sid -notin @($members.SID.Value)) { Add-LocalGroupMember -Group $usersGroup -Member $user }
    $adminsGroup=Get-LocalGroup -SID 'S-1-5-32-544'
    $adminMembers=@(Get-LocalGroupMember -Group $adminsGroup)
    $result.account_admin_member=$sid -in @($adminMembers.SID.Value)
    if ($result.account_admin_member -or $sid -ceq $harnessIdentity.User.Value) { throw 'Test account is not an independent standard account.' }
    & $policyScript -Mode Enable -TestSID $sid
    function Grant-TestAccess([string]$Path,[Security.AccessControl.FileSystemRights]$Rights) {
        $acl=Get-Acl -LiteralPath $Path
        $rule=[Security.AccessControl.FileSystemAccessRule]::new(
            [Security.Principal.SecurityIdentifier]::new($sid),$Rights,
            [Security.AccessControl.InheritanceFlags]'ContainerInherit,ObjectInherit',
            [Security.AccessControl.PropagationFlags]::None,[Security.AccessControl.AccessControlType]::Allow)
        $acl.AddAccessRule($rule)
        Set-Acl -LiteralPath $Path -AclObject $acl
    }
    $workerEvidence=Join-Path $evidence 'worker'
    New-Item -ItemType Directory -Path $workerEvidence | Out-Null
    Grant-TestAccess $workerEvidence ([Security.AccessControl.FileSystemRights]::Modify)
    Grant-TestAccess $inputs ([Security.AccessControl.FileSystemRights]::ReadAndExecute)
    $mediaRoot=Split-Path -Parent $plan.media[0].setup
    Grant-TestAccess (Split-Path -Parent $mediaRoot) ([Security.AccessControl.FileSystemRights]::ReadAndExecute)
    $config=Join-Path $evidence 'worker-config.json'
    @{
        schema=1; approval_scope='DISPOSABLE-GH-WINDOWS-LIFECYCLE'
        source_revision=$env:REVISION; workflow_revision=$env:WORKFLOW_REVISION
        run_id=$env:GITHUB_RUN_ID; run_attempt=$env:GITHUB_RUN_ATTEMPT
        expected_sid=$sid; account=$account; harness_sid=$harnessIdentity.User.Value
        evidence=$workerEvidence; media=$plan.media
    } | ConvertTo-Json -Depth 10 | Set-Content -Encoding utf8 -LiteralPath $config
    $configAcl=Get-Acl -LiteralPath $config
    $configAcl.AddAccessRule([Security.AccessControl.FileSystemAccessRule]::new(
        [Security.Principal.SecurityIdentifier]::new($sid),[Security.AccessControl.FileSystemRights]::Read,
        [Security.AccessControl.AccessControlType]::Allow))
    Set-Acl -LiteralPath $config -AclObject $configAcl
    # Ancestor traversal/read only; product writes occur in its real profile or
    # the explicit worker evidence directory, never in the harness HKCU.
    Grant-TestAccess $evidence ([Security.AccessControl.FileSystemRights]::ReadAndExecute)
    $before=@(Get-CimInstance Win32_Process | Where-Object { $_.Name -in @('GSPconnect.exe','vxbridge.exe') } |
        Select-Object ProcessId,ParentProcessId,CreationDate,Name,ExecutablePath)
    $before | ConvertTo-Json -Depth 4 | Set-Content -Encoding utf8 -LiteralPath (Join-Path $evidence 'product-processes-before.json')
    if ($before.Count) { throw 'Disposable host already has a product-like process; no takeover.' }
    $trace=Join-Path $evidence 'process-starts.jsonl'
    $watch=Register-CimIndicationEvent -Query 'SELECT * FROM Win32_ProcessStartTrace' -SourceIdentifier $subscription -MessageData $trace -Action {
        $new=$Event.SourceEventArgs.NewEvent
        if ($new.ProcessName -in @('GSPconnect.exe','vxbridge.exe','setup.exe','msiexec.exe','lifecycle-worker.exe','whoami.exe')) {
            $record=@{
                name=[string]$new.ProcessName; pid=[uint32]$new.ProcessID; parent_pid=[uint32]$new.ParentProcessID
                session=[uint32]$new.SessionID; utc=[DateTime]::FromFileTimeUtc([long]$new.TIME_CREATED).ToString('o')
            }
            if ($null -ne $new.Sid -and @($new.Sid).Count) {
                $record['sid']=[Security.Principal.SecurityIdentifier]::new([byte[]]$new.Sid,0).Value
            } else { $record['sid_status']='not supplied by process trace' }
            $record | ConvertTo-Json -Compress | Add-Content -Encoding utf8 -LiteralPath $Event.MessageData
        }
    }
    $credential=[pscredential]::new("$env:COMPUTERNAME\$account",$secure)
    $worker=Start-Process -FilePath (Join-Path $inputs 'lifecycle-worker.exe') `
        -ArgumentList @('-config',"`"$config`"") -Credential $credential -LoadUserProfile -PassThru `
        -WorkingDirectory $workerEvidence -RedirectStandardOutput (Join-Path $evidence 'worker-stdout.log') `
        -RedirectStandardError (Join-Path $evidence 'worker-stderr.log')
    $result.worker_pid=$worker.Id
    $password=$null; $secure=$null; $credential=$null
    $waitClock=[Diagnostics.Stopwatch]::StartNew()
    $finished=$false
    while ($waitClock.ElapsedMilliseconds -lt 2700000) {
        if ($worker.WaitForExit(100)) { $finished=$true;break }
        if (-not $result.profile -and (Test-Path -LiteralPath (Join-Path $workerEvidence 'actual-user-context.json'))) {
            $profiles=@(Get-CimInstance Win32_UserProfile -Filter "SID='$sid'")
            if ($profiles.Count -ne 1 -or -not $profiles[0].Loaded) { throw 'Actual standard-user profile must be loaded during execution.' }
            $result.profile=$profiles[0].LocalPath
        }
    }
    if (-not $finished) {
        $result['product_state_may_still_change']=$true
        throw 'Lifecycle worker did not finish in45min; no product kill/account cleanup performed; state may be changing.'
    }
    $worker.Refresh(); $result.worker_exit=$worker.ExitCode
    $marker=Start-Process -FilePath "$env:SystemRoot\System32\whoami.exe" -PassThru -Wait -NoNewWindow `
        -RedirectStandardOutput (Join-Path $evidence 'watcher-watermark.log')
    $limit=[DateTime]::UtcNow.AddSeconds(20)
    $records=@()
    do {
        if (Test-Path -LiteralPath $trace) { $records=@(Get-Content -LiteralPath $trace | ForEach-Object { $_ | ConvertFrom-Json }) }
        if (@($records | Where-Object { $_.pid -eq $marker.Id -and $_.name -ieq 'whoami.exe' }).Count) {
            $result.process_trace_complete=$true; break
        }
        Start-Sleep -Milliseconds 100
    } while ([DateTime]::UtcNow -lt $limit)
    if (-not $result.process_trace_complete -or @($watch.Error).Count) { throw 'Process-start evidence is incomplete.' }
    $result.implicit_product_launches=@($records | Where-Object { $_.name -in @('GSPconnect.exe','vxbridge.exe') })
    if ($result.implicit_product_launches.Count) { throw 'An excluded bridge/control executable was implicitly launched.' }
    if ($result.worker_exit -ne 0) {
        $workerResultPath=Join-Path $workerEvidence 'lifecycle-result.json'
        if (Test-Path -LiteralPath $workerResultPath) {
            $partial=Get-Content -Raw -LiteralPath $workerResultPath | ConvertFrom-Json
            $result['product_state_may_still_change']=$partial.state_may_still_be_changing
        }
        $workerFailure=Get-Content -Raw -LiteralPath (Join-Path $evidence 'worker-stderr.log')
        $result['worker_failure']=$workerFailure
        throw "Private lifecycle worker exit=$($result.worker_exit): $workerFailure"
    }
    $context=Get-Content -Raw -LiteralPath (Join-Path $workerEvidence 'actual-user-context.json') | ConvertFrom-Json
    $profile=@(Get-CimInstance Win32_UserProfile -Filter "SID='$sid'")
    if ($profile.Count -ne 1 -or $context.sid -cne $sid -or $context.token_elevated -ne 0 -or
        $context.administrators_sid_present_in_token_groups -or $context.token_elevation_type -ne 1 -or
        $profile[0].LocalPath -ine $context.profile) { throw 'Independent SID/token/profile evidence differs.' }
    $result.profile=$profile[0].LocalPath
    $lifecycle=Get-Content -Raw -LiteralPath (Join-Path $workerEvidence 'lifecycle-result.json') | ConvertFrom-Json
    $accepted=$lifecycle.success -and (Test-Path -LiteralPath (Join-Path $workerEvidence '10-final-clean-uninstall-audit.json'))
    if ($result.worker_exit -ne 0 -or -not $accepted -or $lifecycle.source_revision -cne $env:REVISION -or
        $lifecycle.run_id -cne $env:GITHUB_RUN_ID -or $lifecycle.run_attempt -cne '1') {
        throw 'Actual lifecycle coverage failed; preserve state before any new supported operation.'
    }
    $result.success=$true
} catch {
    $result.failure=$_.Exception.Message
    if ($worker -and -not $worker.HasExited) { $result['product_state_may_still_change']=$true }
    throw
} finally {
    try {
        & $policyScript -Mode Restore
        $policyPath=Join-Path $evidence 'hosted-msi-policy.json'
        if (Test-Path -LiteralPath $policyPath) {
            $policy=Get-Content -Raw -LiteralPath $policyPath | ConvertFrom-Json
            $result['policy_restored']=$policy.status -ceq 'restored'
            $result['environment_qualification']=$policy.environment
            if (-not $result.policy_restored) { throw 'Policy restoration is unproved.' }
        }
    } catch {
        $result.success=$false
        $result['policy_restoration_failure']=$_.Exception.Message
        if (-not $result.failure) { $result.failure=$_.Exception.Message }
    }
    if ($watch) {
        try {
            Unregister-Event -SourceIdentifier $subscription -ErrorAction Stop
            Remove-Job -Job $watch -Force -ErrorAction Stop
        } catch {
            $result.success=$false
            if (-not $result.failure) { $result.failure=$_.Exception.Message }
            $result['observer_cleanup_failure']=$_.Exception.Message
        }
    }
    $result | ConvertTo-Json -Depth 8 | Set-Content -Encoding utf8 -LiteralPath (Join-Path $evidence 'hosted-lifecycle-result.json')
    # Deliberately do not remove product residue, the test account/profile or
    # OS Installer cache. Their destruction is never uninstall evidence.
}
if (-not $result.success) { throw 'Disposable lifecycle did not complete; see preserved first failure/state.' }

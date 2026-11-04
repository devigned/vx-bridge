param([Parameter(Mandatory = $true)][string]$Out)
Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
. (Join-Path $PSScriptRoot 'native-tools.ps1')
$outputScopeError='Test output must be a new absolute external directory with an existing parent.'
if (-not [IO.Path]::IsPathFullyQualified($Out)) { throw $outputScopeError }
$root = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$output = [IO.Path]::GetFullPath($Out)
if ((Test-Path -LiteralPath $output) -or
    $output.Equals($root,[StringComparison]::OrdinalIgnoreCase) -or
    $output.StartsWith($root + [IO.Path]::DirectorySeparatorChar,[StringComparison]::OrdinalIgnoreCase) -or
    -not (Test-Path -LiteralPath (Split-Path -Parent $output) -PathType Container)) {
    throw $outputScopeError
}
$Out=$output
New-Item -ItemType Directory -Path $Out | Out-Null
if ($env:OS -ceq 'Windows_NT') {
    foreach ($invalidOut in @('C:drive-relative-test','\current-drive-rooted-test')) {
        $refused=$false
        try { & $PSCommandPath -Out $invalidOut }
        catch {
            if ($_.Exception.Message -cne $outputScopeError) { throw }
            $refused=$true
        }
        if (-not $refused) { throw "Unqualified Windows output was accepted: $invalidOut" }
    }
}
foreach ($file in @((Join-Path $PSScriptRoot 'build-setup.ps1'),
    (Join-Path $PSScriptRoot 'native-tools.ps1'))) {
    $tokens = $null; $errors = $null
    [void][Management.Automation.Language.Parser]::ParseFile($file, [ref]$tokens, [ref]$errors)
    if ($errors) { throw "PowerShell parse errors in $file : $errors" }
}
$runner = (Get-Process -Id $PID).Path
foreach ($code in @(0, 23)) {
    $child = Join-Path $Out "invented-child-$code.ps1"
    @"
[Console]::Out.WriteLine('invented stdout diagnostic')
[Console]::Error.WriteLine('invented stderr ICE-other diagnostic')
exit $code
"@ | Set-Content -Encoding utf8 -LiteralPath $child
    $log = Join-Path $Out "native-$code.log"
    $caught = $null
    try {
        $result = Invoke-LoggedNativeTool -FilePath $runner -Arguments @('-NoLogo', '-NoProfile', '-File', $child) -LogPath $log
    }
    catch {
        $caught = $_.Exception
        if ($code -eq 0) { throw }
    }
    $text = Get-Content -Raw -LiteralPath $log
    if ($text -notmatch 'invented stdout diagnostic' -or
        $text -notmatch 'invented stderr ICE-other diagnostic' -or
        $text -notmatch "\[exit\] tool=.+ code=$code") {
        throw 'Evidence lost stdout, stderr or exact native exit.'
    }
    if ($code -eq 23) {
        if ($null -eq $caught -or $caught.Data['NativeToolExitCode'] -ne 23 -or
            $caught.Data['NativeTool'] -ne [IO.Path]::GetFileName($runner)) {
            throw 'Nonzero exit was swallowed or lost its native failure identity.'
        }
    }
    elseif ($null -ne $caught -or ($result -join "`n") -notmatch 'invented stdout diagnostic') {
        throw 'Successful output capture changed tool success semantics.'
    }
}
@{
    scope = 'private logging/parse output-contract units only; no WiX, COM, MSI, Windows API or callback execution'
    powershell = $PSVersionTable.PSVersion.ToString()
    platform = $PSVersionTable.Platform
    fully_qualified_refusals = if ($env:OS -ceq 'Windows_NT') { 2 } else { 0 }
    result = 'passed'
} | ConvertTo-Json | Set-Content -Encoding utf8 -LiteralPath (Join-Path $Out 'logging-test.json')
Write-Output 'Native stdout/stderr/exit and failure propagation regression passed. No Windows/MSI/tool invocation.'

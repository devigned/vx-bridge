Set-StrictMode -Version Latest
$ErrorActionPreference='Stop'
$source=(Resolve-Path (Join-Path $PSScriptRoot '..\..\..')).Path
foreach ($relative in @('installer/build-setup.ps1','installer/native-tools.ps1',
    'tools/msi/lifecycle/setup-lifecycle-author.ps1','tools/msi/lifecycle/setup-lifecycle-run.ps1',
    'tools/msi/lifecycle/setup-lifecycle-policy.ps1')) {
    $tokens=$null; $errors=$null
    [void][System.Management.Automation.Language.Parser]::ParseFile(
        (Join-Path $source $relative),[ref]$tokens,[ref]$errors)
    if (@($errors).Count) { throw "Lifecycle script parse failed: $relative $errors" }
}

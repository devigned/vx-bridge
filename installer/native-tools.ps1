function Invoke-LoggedNativeTool {
    param(
        [Parameter(Mandatory = $true)][string]$FilePath,
        [Parameter(Mandatory = $true)][string[]]$Arguments,
        [Parameter(Mandatory = $true)][string]$LogPath
    )
    $ErrorActionPreference = 'Stop'
    # Capture the tool's exit explicitly instead of PowerShell promoting it first.
    $PSNativeCommandUseErrorActionPreference = $false
    $label = [IO.Path]::GetFileName($FilePath)
    $command = @{ tool = $FilePath; arguments = $Arguments } | ConvertTo-Json -Compress
    Add-Content -LiteralPath $LogPath -Encoding utf8 -Value "[command] $command"
    Write-Host "[native tool] $label"
    & $FilePath @Arguments 2>&1 | ForEach-Object {
        $line = $_.ToString()
        if ((Get-Item -LiteralPath $LogPath).Length + [Text.Encoding]::UTF8.GetByteCount($line) + 2 -gt 2MB) {
            throw 'Native tool evidence exceeded its 2 MiB bound; build refused.'
        }
        Add-Content -LiteralPath $LogPath -Encoding utf8 -Value $line
        $line
    }
    $code = $LASTEXITCODE
    Add-Content -LiteralPath $LogPath -Encoding utf8 -Value "[exit] tool=$label code=$code"
    Write-Host "[native exit] $label code=$code"
    if ($code -ne 0) {
        $failure = [InvalidOperationException]::new("Native tool $label failed with exit code $code. Output is in $LogPath")
        $failure.Data['NativeTool'] = $label
        $failure.Data['NativeToolExitCode'] = $code
        throw $failure
    }
}

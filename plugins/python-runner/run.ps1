# Python Runner Plugin - execute Python code
# Usage: run.ps1 run "<python code>"

param(
    [string]$Command,
    [Parameter(ValueFromRemainingArguments)]$RemainingArgs
)

switch ($Command) {
    "run" {
        $code = $RemainingArgs -join " "
        $tmpFile = [System.IO.Path]::GetTempFileName() + ".py"
        $code | Out-File -FilePath $tmpFile -Encoding UTF8
        python3 $tmpFile 2>&1
        $exitCode = $LASTEXITCODE
        Remove-Item $tmpFile -Force -ErrorAction SilentlyContinue
        exit $exitCode
    }
    default {
        Write-Host "Unknown command: $Command"
        Write-Host "Available: run"
        exit 1
    }
}

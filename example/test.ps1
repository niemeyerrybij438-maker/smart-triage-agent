$ErrorActionPreference = "Stop"
$exampleDir = Split-Path -Parent $MyInvocation.MyCommand.Path

Push-Location $exampleDir
try {
    Write-Host "Running smart triage tests..."
    go test ./sse
    if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }

    Write-Host "Running static checks..."
    go vet ./sse
    if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }

    Write-Host "All checks passed."
}
finally {
    Pop-Location
}

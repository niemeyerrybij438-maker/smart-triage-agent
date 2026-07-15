param(
    [switch]$Coverage,
    [switch]$Acceptance,
    [switch]$RequireDemoData,
    [string]$BaseUrl = "http://localhost:8080"
)

$ErrorActionPreference = "Stop"
$exampleDir = Split-Path -Parent $MyInvocation.MyCommand.Path

Push-Location $exampleDir
try {
    Write-Host "Running smart triage tests..."
    if ($Coverage) {
        & go test ./sse -count=1 -coverprofile coverage.out
        if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
        & go tool cover -func coverage.out | Select-Object -Last 1
        if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
    }
    else {
        & go test ./sse -count=1
        if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
    }

    Write-Host "Running performance tool tests..."
    & go test ./cmd/perfcheck -count=1
    if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }

    Write-Host "Running static checks..."
    & go vet ./sse ./cmd/perfcheck
    if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }

    if ($Acceptance) {
        Write-Host "Running three-end acceptance checks..."
        $acceptanceArgs = @{ BaseUrl = $BaseUrl }
        if ($RequireDemoData) { $acceptanceArgs.RequireDemoData = $true }
        & (Join-Path $exampleDir "acceptance.ps1") @acceptanceArgs
        if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
    }

    Write-Host "All checks passed."
}
finally {
    Pop-Location
}
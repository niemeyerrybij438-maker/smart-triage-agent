param(
    [int]$Port = 0
)

$ErrorActionPreference = "Stop"
$exampleDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$repoDir = Split-Path -Parent $exampleDir

if ($Port -gt 0) {
    $env:PORT = $Port.ToString()
}

if (-not $env:BaseUrl -or -not $env:APIKey) {
    $envFile = Join-Path $repoDir ".env"
    if (-not (Test-Path -LiteralPath $envFile)) {
        Write-Error "Missing model configuration. Copy example/.env.example to .env and configure BaseUrl and APIKey."
    }
}

$effectivePort = $env:PORT
if (-not $effectivePort) {
    $effectivePort = "8080"
}

Write-Host "Starting smart triage service..."
Write-Host "Patient: http://localhost:$effectivePort"
Write-Host "Doctor:  http://localhost:$effectivePort/doctor"

Push-Location $exampleDir
try {
    go run ./sse
}
finally {
    Pop-Location
}

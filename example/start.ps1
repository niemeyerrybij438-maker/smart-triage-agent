param(
    [int]$Port = 0
)

$ErrorActionPreference = "Stop"
$exampleDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$repoDir = Split-Path -Parent $exampleDir
$envFile = Join-Path $repoDir ".env"

if ($Port -gt 0) {
    $env:PORT = $Port.ToString()
}

function Test-ConfiguredValue {
    param([Parameter(Mandatory = $true)][string]$Name)

    $processValue = [Environment]::GetEnvironmentVariable($Name, "Process")
    if (-not [string]::IsNullOrWhiteSpace($processValue)) {
        return $true
    }

    if (-not (Test-Path -LiteralPath $envFile)) {
        return $false
    }

    $pattern = '^\s*' + [regex]::Escape($Name) + '\s*=\s*(?!\s*(?:#|$))(.+?)\s*$'
    return $null -ne (Select-String -LiteralPath $envFile -Pattern $pattern -Encoding UTF8 | Select-Object -First 1)
}

$missingConfiguration = @("BaseUrl", "APIKey", "MYSQL_DSN") | Where-Object { -not (Test-ConfiguredValue -Name $_) }
if ($missingConfiguration.Count -gt 0) {
    $missingText = $missingConfiguration -join ", "
    Write-Error "Missing required configuration: $missingText. Copy example/.env.example to .env and configure the missing values."
}

$effectivePort = $env:PORT
if (-not $effectivePort) {
    $effectivePort = "8080"
}

Write-Host "Starting smart triage service..."
Write-Host "Patient: http://localhost:$effectivePort"
Write-Host "Doctor:  http://localhost:$effectivePort/doctor/login"
Write-Host "Admin:   http://localhost:$effectivePort/admin/login"
Write-Host "Health:  http://localhost:$effectivePort/health"
Write-Host "Ready:   http://localhost:$effectivePort/ready"

Push-Location $exampleDir
try {
    go run ./sse
}
finally {
    Pop-Location
}
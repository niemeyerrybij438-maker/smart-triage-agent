param(
    [string]$BaseUrl = "http://localhost:8080",
    [string]$PatientPhone = "19552075183",
    [switch]$RequireDemoData
)

$ErrorActionPreference = "Stop"
$BaseUrl = $BaseUrl.TrimEnd("/")
$results = [System.Collections.Generic.List[object]]::new()

function Get-ConfiguredValue([string]$Name, [string]$Fallback) {
    $value = [Environment]::GetEnvironmentVariable($Name)
    if ([string]::IsNullOrWhiteSpace($value)) { return $Fallback }
    return $value
}

function Assert-True([bool]$Condition, [string]$Message) {
    if (-not $Condition) { throw $Message }
}

function Invoke-AcceptanceStep([string]$Name, [scriptblock]$Action) {
    $watch = [System.Diagnostics.Stopwatch]::StartNew()
    try {
        & $Action
        $watch.Stop()
        $results.Add([pscustomobject]@{ Step = $Name; Status = "PASS"; DurationMs = $watch.ElapsedMilliseconds; Detail = "" })
    }
    catch {
        $watch.Stop()
        $results.Add([pscustomobject]@{ Step = $Name; Status = "FAIL"; DurationMs = $watch.ElapsedMilliseconds; Detail = $_.Exception.Message })
    }
}

function Invoke-JsonRequest {
    param(
        [string]$Path,
        [string]$Method = "GET",
        [Microsoft.PowerShell.Commands.WebRequestSession]$Session,
        [object]$Body
    )
    $parameters = @{ Uri = "$BaseUrl$Path"; Method = $Method; WebSession = $Session; TimeoutSec = 20 }
    if ($null -ne $Body) {
        $parameters.ContentType = "application/json"
        $parameters.Body = $Body | ConvertTo-Json -Depth 8 -Compress
    }
    return Invoke-RestMethod @parameters
}

$doctorUsername = Get-ConfiguredValue "DOCTOR_USERNAME" "doctor"
$doctorPassword = Get-ConfiguredValue "DOCTOR_PASSWORD" "doctor123"
$adminUsername = Get-ConfiguredValue "ADMIN_USERNAME" "medical_admin"
$adminPassword = Get-ConfiguredValue "ADMIN_PASSWORD" "MedTriage@2026!88"

$script:patientSession = New-Object Microsoft.PowerShell.Commands.WebRequestSession
$script:doctorSession = New-Object Microsoft.PowerShell.Commands.WebRequestSession
$script:adminSession = New-Object Microsoft.PowerShell.Commands.WebRequestSession

Invoke-AcceptanceStep "Health and readiness" {
    $health = Invoke-JsonRequest -Path "/health"
    Assert-True ($health.status -eq "ok") "Health endpoint did not return ok"
    $readiness = Invoke-JsonRequest -Path "/ready"
    Assert-True ($readiness.status -eq "ready") "Readiness endpoint did not return ready"
    Assert-True ($readiness.checks.database -eq "ok") "Readiness database check failed"
}

Invoke-AcceptanceStep "Public login pages" {
    foreach ($path in @("/patient/login", "/doctor/login", "/admin/login")) {
        $response = Invoke-WebRequest "$BaseUrl$path" -UseBasicParsing -TimeoutSec 20
        Assert-True ($response.StatusCode -eq 200) "$path returned HTTP $($response.StatusCode)"
    }
}

Invoke-AcceptanceStep "Patient authentication" {
    $sms = Invoke-JsonRequest -Path "/api/patient/sms" -Method "POST" -Session $script:patientSession -Body @{ phone = $PatientPhone }
    Assert-True (-not [string]::IsNullOrWhiteSpace($sms.debugCode)) "Local SMS endpoint did not return debugCode"
    $profile = Invoke-JsonRequest -Path "/api/patient/login" -Method "POST" -Session $script:patientSession -Body @{ phone = $PatientPhone; code = $sms.debugCode }
    Assert-True ($profile.phone -eq $PatientPhone) "Patient login returned an unexpected phone"
}

Invoke-AcceptanceStep "Patient read-only APIs" {
    $script:patientProfile = Invoke-JsonRequest -Path "/api/patient/me" -Session $script:patientSession
    $script:patientChats = Invoke-JsonRequest -Path "/api/patient/chats" -Session $script:patientSession
    $script:patientRecords = @(Invoke-JsonRequest -Path "/api/triage-records" -Session $script:patientSession)
    $script:patientFollowUps = @(Invoke-JsonRequest -Path "/api/follow-ups" -Session $script:patientSession)
    $script:patientNotices = Invoke-JsonRequest -Path "/api/follow-up-notifications" -Session $script:patientSession
    $script:patientAppointments = @(Invoke-JsonRequest -Path "/api/appointments" -Session $script:patientSession)
    Assert-True ($script:patientProfile.phone -eq $PatientPhone) "Patient profile isolation failed"
    Assert-True ($null -ne $script:patientChats.items) "Patient chat payload is missing items"
    Assert-True ($null -ne $script:patientNotices.items) "Patient notification payload is missing items"
}

Invoke-AcceptanceStep "Doctor authentication and APIs" {
    $login = Invoke-JsonRequest -Path "/api/doctor/login" -Method "POST" -Session $script:doctorSession -Body @{ username = $doctorUsername; password = $doctorPassword }
    $me = Invoke-JsonRequest -Path "/api/doctor/me" -Session $script:doctorSession
    $script:doctorRecords = @(Invoke-JsonRequest -Path "/api/triage-records" -Session $script:doctorSession)
    $script:doctorFollowUps = @(Invoke-JsonRequest -Path "/api/follow-ups" -Session $script:doctorSession)
    $script:doctorTickets = @(Invoke-JsonRequest -Path "/api/escalation-tickets" -Session $script:doctorSession)
    Assert-True ($me.username -eq $doctorUsername) "Doctor session identity mismatch"
}

Invoke-AcceptanceStep "Admin authentication and APIs" {
    $login = Invoke-JsonRequest -Path "/api/admin/login" -Method "POST" -Session $script:adminSession -Body @{ username = $adminUsername; password = $adminPassword }
    $me = Invoke-JsonRequest -Path "/api/admin/me" -Session $script:adminSession
    $script:adminDashboard = Invoke-JsonRequest -Path "/api/admin/dashboard" -Session $script:adminSession
    $script:adminRecords = @(Invoke-JsonRequest -Path "/api/admin/records" -Session $script:adminSession)
    $script:adminFollowUps = Invoke-JsonRequest -Path "/api/admin/follow-ups" -Session $script:adminSession
    $script:adminTraces = Invoke-JsonRequest -Path "/api/admin/agent-traces" -Session $script:adminSession
    $script:adminKnowledge = @(Invoke-JsonRequest -Path "/api/admin/medical-knowledge" -Session $script:adminSession)
    Assert-True ($me.username -eq $adminUsername) "Admin session identity mismatch"
    Assert-True ($null -ne $script:adminFollowUps.plans) "Admin follow-up payload is missing plans"
    Assert-True ($null -ne $script:adminTraces.items) "Admin trace payload is missing items"
}

Invoke-AcceptanceStep "Cross-end data consistency" {
    $adminPlans = @($script:adminFollowUps.plans)
    Assert-True ($script:adminRecords.Count -ge $script:patientRecords.Count) "Admin records do not cover patient records"
    Assert-True ($script:doctorRecords.Count -ge $script:patientRecords.Count) "Doctor records do not cover patient records"
    Assert-True ($adminPlans.Count -ge $script:patientFollowUps.Count) "Admin follow-ups do not cover patient follow-ups"
    Assert-True ($script:doctorFollowUps.Count -ge $script:patientFollowUps.Count) "Doctor follow-ups do not cover patient follow-ups"
    Assert-True ($script:adminKnowledge.Count -gt 0) "RAG knowledge base is empty"
}

if ($RequireDemoData) {
    Invoke-AcceptanceStep "Resume demo data baseline" {
        $patientResolved = @($script:patientFollowUps | Where-Object { $null -ne $_.resolvedAt })
        $doctorResolved = @($script:doctorFollowUps | Where-Object { $null -ne $_.resolvedAt })
        $adminResolved = @($script:adminFollowUps.plans | Where-Object { $null -ne $_.resolvedAt })
        Assert-True ($script:patientRecords.Count -gt 0) "No patient triage records found"
        Assert-True ($script:patientFollowUps.Count -gt 0) "No patient follow-up plans found"
        Assert-True ($patientResolved.Count -gt 0) "No patient-visible doctor resolution found"
        Assert-True ($doctorResolved.Count -ge $patientResolved.Count) "Doctor resolution count is inconsistent"
        Assert-True ($adminResolved.Count -ge $patientResolved.Count) "Admin resolution count is inconsistent"
        Assert-True (@($script:adminTraces.items).Count -gt 0) "No Agent traces found"
    }
}

$results | Format-Table -AutoSize
$failed = @($results | Where-Object Status -eq "FAIL")
$passed = $results.Count - $failed.Count
Write-Host "Acceptance summary: $passed/$($results.Count) passed"
if ($failed.Count -gt 0) { exit 1 }
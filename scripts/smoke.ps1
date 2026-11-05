# Creates one real fictional record per run. Requires a running REST gateway.
[CmdletBinding()]
param(
    [string]$BaseUrl = 'http://localhost:8080',
    [ValidateRange(1, 120)]
    [int]$TimeoutSeconds = 10
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

function Assert-Check {
    param([bool]$Condition, [string]$Message)
    if (-not $Condition) { throw $Message }
}

try {
    $base = $BaseUrl.TrimEnd('/')
    $clientRequestId = [guid]::NewGuid().ToString()
    $payload = @{
        client_request_id = $clientRequestId
        employee_id = 'emp-smoke-demo'
        item_name = 'Fictional smoke-test desk lamp'
        quantity = 2
        unit_price_cents = 2500
    } | ConvertTo-Json
    $http = @{ TimeoutSec = $TimeoutSeconds; ErrorAction = 'Stop' }

    Write-Host "Creating synthetic purchase with client_request_id=$clientRequestId"
    $created = Invoke-RestMethod @http -Method Post -Uri "$base/api/purchases" -ContentType 'application/json' -Body $payload
    Assert-Check (-not [string]::IsNullOrWhiteSpace($created.id)) 'Create returned no purchase ID'
    $id = $created.id
    Assert-Check ($created.client_request_id -ceq $clientRequestId) 'Create returned a different request key'
    Assert-Check ($created.status -ceq 'PENDING') 'Created purchase is not PENDING'
    Assert-Check ($created.quantity -eq 2 -and $created.unit_price_cents -eq 2500 -and $created.total_cents -eq 5000) 'Create returned incorrect integer-cent amounts'

    $fetched = Invoke-RestMethod @http -Uri "$base/api/purchases/$id"
    Assert-Check ($fetched.id -ceq $id -and $fetched.client_request_id -ceq $clientRequestId -and $fetched.status -ceq 'PENDING' -and $fetched.total_cents -eq 5000) 'Fetched purchase differs from the created purchase'
    $listed = Invoke-RestMethod @http -Uri "$base/api/purchases"
    $matching = @($listed.purchases | Where-Object { $_.id -ceq $id })
    Assert-Check ($matching.Count -eq 1) 'List must contain the created purchase exactly once'
    Assert-Check ($matching[0].status -ceq 'PENDING' -and $matching[0].total_cents -eq 5000) 'Listed purchase has incorrect status or total'

    # DEMO-ONLY identity; this is not authentication or persisted manager metadata.
    $decision = @{ manager_id = 'manager-smoke-demo'; decision = 'APPROVED'; comment = 'Fictional repeatable smoke check' } | ConvertTo-Json
    $approved = Invoke-RestMethod @http -Method Post -Uri "$base/api/purchases/$id/decision" -ContentType 'application/json' -Body $decision
    Assert-Check ($approved.purchase_id -ceq $id -and $approved.status -ceq 'APPROVED') 'Approval response has incorrect identity or status'
    $saved = Invoke-RestMethod @http -Uri "$base/api/purchases/$id"
    Assert-Check ($saved.id -ceq $id -and $saved.status -ceq 'APPROVED' -and $saved.total_cents -eq 5000) 'Purchase did not become APPROVED with the original total'

    $repeated = Invoke-RestMethod @http -Method Post -Uri "$base/api/purchases/$id/decision" -ContentType 'application/json' -Body $decision
    Assert-Check ($repeated.purchase_id -ceq $id -and $repeated.status -ceq 'APPROVED') 'Repeated identical decision was not safe'
    $afterRetry = Invoke-RestMethod @http -Uri "$base/api/purchases/$id"
    Assert-Check ($afterRetry.id -ceq $id -and $afterRetry.status -ceq 'APPROVED' -and $afterRetry.client_request_id -ceq $clientRequestId -and $afterRetry.total_cents -eq 5000) 'Repeated decision changed the saved purchase'

    Write-Host "PASS: purchase $id created, fetched, listed, approved, read back, and safely approved again (5000 cents)."
    exit 0
} catch {
    Write-Error "Smoke check failed: $($_.Exception.Message)" -ErrorAction Continue
    exit 1
}

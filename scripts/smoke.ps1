# Creates fictional workflow/security-check records. Requires a running REST gateway.
[CmdletBinding()]
param(
    [string]$BaseUrl = 'http://localhost:8080',
    [string]$CredentialsFile = '',
    [ValidateRange(1, 120)]
    [int]$TimeoutSeconds = 10
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
if ([string]::IsNullOrWhiteSpace($CredentialsFile)) { $CredentialsFile = Join-Path $PSScriptRoot '../secrets/demo-credentials.json' }

function Assert-Check {
    param([bool]$Condition, [string]$Message)
    if (-not $Condition) { throw $Message }
}

function Assert-HttpStatus {
    param([int]$Expected, [string]$Uri, [hashtable]$Options)
    try {
        Invoke-RestMethod -Uri $Uri @Options | Out-Null
    } catch {
        $responseProperty = $_.Exception.PSObject.Properties['Response']
        if ($null -ne $responseProperty -and $null -ne $responseProperty.Value -and [int]$responseProperty.Value.StatusCode -eq $Expected) { return }
        throw "Expected HTTP $Expected at $Uri; $($_.Exception.Message)"
    }
    throw "Expected HTTP $Expected at $Uri but the request succeeded"
}

try {
    $base = $BaseUrl.TrimEnd('/')
    $credentials = Get-Content -LiteralPath $CredentialsFile -Raw | ConvertFrom-Json
    $employee = @($credentials | Where-Object username -eq 'employee.demo')[0]
    $manager = @($credentials | Where-Object username -eq 'manager.demo')[0]
    $employeeSession = New-Object Microsoft.PowerShell.Commands.WebRequestSession
    $managerSession = New-Object Microsoft.PowerShell.Commands.WebRequestSession
    $employeeLogin = Invoke-RestMethod -Method Post -Uri "$base/api/auth/login" -WebSession $employeeSession -ContentType 'application/json' -TimeoutSec $TimeoutSeconds -Body (@{username=$employee.username; password=$employee.password} | ConvertTo-Json)
    $managerLogin = Invoke-RestMethod -Method Post -Uri "$base/api/auth/login" -WebSession $managerSession -ContentType 'application/json' -TimeoutSec $TimeoutSeconds -Body (@{username=$manager.username; password=$manager.password} | ConvertTo-Json)
    $employeeHeaders = @{'X-CSRF-Token'=$employeeLogin.csrf_token}
    $managerHeaders = @{'X-CSRF-Token'=$managerLogin.csrf_token}
    $clientRequestId = [guid]::NewGuid().ToString()
    $payload = @{
        client_request_id = $clientRequestId
        employee_id = $employeeLogin.user.id
        item_name = 'Fictional smoke-test desk lamp'
        quantity = 2
        unit_price_cents = 2500
    } | ConvertTo-Json
    $http = @{ TimeoutSec = $TimeoutSeconds; ErrorAction = 'Stop'; WebSession = $employeeSession; Headers = $employeeHeaders }
    $managerHttp = @{ TimeoutSec = $TimeoutSeconds; ErrorAction = 'Stop'; WebSession = $managerSession; Headers = $managerHeaders }

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

    Assert-HttpStatus -Expected 401 -Uri "$base/api/purchases" -Options @{TimeoutSec=$TimeoutSeconds}
    Assert-HttpStatus -Expected 403 -Uri "$base/api/purchases/$id/decision" -Options ($http + @{Method='Post';ContentType='application/json';Body='{"decision":"APPROVED"}'})
    $noCsrfSession = New-Object Microsoft.PowerShell.Commands.WebRequestSession
    $noCsrfSession.Cookies = $employeeSession.Cookies
    Assert-HttpStatus -Expected 403 -Uri "$base/api/purchases" -Options @{TimeoutSec=$TimeoutSeconds;Method='Post';WebSession=$noCsrfSession;ContentType='application/json';Body=$payload}
    $other = @($credentials | Where-Object username -eq 'employee.two')[0]
    $otherSession = New-Object Microsoft.PowerShell.Commands.WebRequestSession
    $otherLogin = Invoke-RestMethod -Method Post -Uri "$base/api/auth/login" -WebSession $otherSession -ContentType 'application/json' -TimeoutSec $TimeoutSeconds -Body (@{username=$other.username;password=$other.password}|ConvertTo-Json)
    Assert-HttpStatus -Expected 404 -Uri "$base/api/purchases/$id" -Options @{TimeoutSec=$TimeoutSeconds;WebSession=$otherSession}
    $otherList = Invoke-RestMethod -Uri "$base/api/purchases" -WebSession $otherSession -TimeoutSec $TimeoutSeconds
    Assert-Check (@($otherList.purchases | Where-Object id -eq $id).Count -eq 0) 'Other employee can list the purchase'
    Invoke-RestMethod -Method Post -Uri "$base/api/auth/logout" -WebSession $otherSession -Headers @{'X-CSRF-Token'=$otherLogin.csrf_token} -ContentType 'application/json' -Body '{}' -TimeoutSec $TimeoutSeconds | Out-Null

    # The server verifies the session identity; manager/comment audit metadata is not persisted.
    $decision = @{ manager_id = $managerLogin.user.id; decision = 'APPROVED'; comment = 'Fictional repeatable smoke check' } | ConvertTo-Json
    $forgedDecision = @{manager_id='forged-manager';decision='APPROVED'} | ConvertTo-Json
    Assert-HttpStatus -Expected 403 -Uri "$base/api/purchases/$id/decision" -Options ($managerHttp + @{Method='Post';ContentType='application/json';Body=$forgedDecision})
    $approved = Invoke-RestMethod @managerHttp -Method Post -Uri "$base/api/purchases/$id/decision" -ContentType 'application/json' -Body $decision
    Assert-Check ($approved.purchase_id -ceq $id -and $approved.status -ceq 'APPROVED') 'Approval response has incorrect identity or status'
    $saved = Invoke-RestMethod @http -Uri "$base/api/purchases/$id"
    Assert-Check ($saved.id -ceq $id -and $saved.status -ceq 'APPROVED' -and $saved.total_cents -eq 5000) 'Purchase did not become APPROVED with the original total'

    $repeated = Invoke-RestMethod @managerHttp -Method Post -Uri "$base/api/purchases/$id/decision" -ContentType 'application/json' -Body $decision
    Assert-Check ($repeated.purchase_id -ceq $id -and $repeated.status -ceq 'APPROVED') 'Repeated identical decision was not safe'
    $afterRetry = Invoke-RestMethod @http -Uri "$base/api/purchases/$id"
    Assert-Check ($afterRetry.id -ceq $id -and $afterRetry.status -ceq 'APPROVED' -and $afterRetry.client_request_id -ceq $clientRequestId -and $afterRetry.total_cents -eq 5000) 'Repeated decision changed the saved purchase'

    $ownPayload = @{client_request_id=[guid]::NewGuid().ToString();employee_id=$managerLogin.user.id;item_name='Fictional self-review security check';quantity=1;unit_price_cents=100} | ConvertTo-Json
    $own = Invoke-RestMethod @managerHttp -Method Post -Uri "$base/api/purchases" -ContentType 'application/json' -Body $ownPayload
    foreach ($status in @('APPROVED','REJECTED')) {
        $ownDecision = @{manager_id=$managerLogin.user.id;decision=$status}|ConvertTo-Json
        Assert-HttpStatus -Expected 403 -Uri "$base/api/purchases/$($own.id)/decision" -Options ($managerHttp + @{Method='Post';ContentType='application/json';Body=$ownDecision})
    }
    $ownRead = Invoke-RestMethod @managerHttp -Uri "$base/api/purchases/$($own.id)"
    Assert-Check ($ownRead.status -ceq 'PENDING') 'Self-review changed the request'
    Invoke-RestMethod @http -Method Post -Uri "$base/api/auth/logout" -ContentType 'application/json' -Body '{}' | Out-Null
    Invoke-RestMethod @managerHttp -Method Post -Uri "$base/api/auth/logout" -ContentType 'application/json' -Body '{}' | Out-Null
    Assert-HttpStatus -Expected 401 -Uri "$base/api/auth/me" -Options $http
    Write-Host "PASS: purchase $id created, fetched, listed, approved, read back, and safely approved again (5000 cents); authentication, CSRF, employee isolation, forged identities and manager self-review checks passed."
    exit 0
} catch {
    Write-Error "Smoke check failed: $($_.Exception.Message)" -ErrorAction Continue
    exit 1
}

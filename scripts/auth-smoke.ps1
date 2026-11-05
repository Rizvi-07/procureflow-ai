# Creates a fictional account and purchases. Never deletes application data.
[CmdletBinding()]
param(
    [string]$BaseUrl = 'http://localhost:8080',
    [string]$AdminCredentialsFile = '',
    [switch]$RestartGateway
)
Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
if (-not $AdminCredentialsFile) { $AdminCredentialsFile = Join-Path $PSScriptRoot '../secrets/admin-credentials.json' }
$base = $BaseUrl.TrimEnd('/')
$createdId = ''

function Assert-Check([bool]$Condition, [string]$Message) {
    if (-not $Condition) { throw $Message }
}
function Post-Json([string]$Path, [hashtable]$Body, [hashtable]$Http = @{}) {
    Invoke-RestMethod @Http -Uri "$base$Path" -Method Post -ContentType application/json -Body ($Body | ConvertTo-Json)
}
function Sign-In([string]$Username, [string]$Password) {
    $session = New-Object Microsoft.PowerShell.Commands.WebRequestSession
    $login = Post-Json '/api/auth/login' @{username=$Username;password=$Password} @{WebSession=$session;TimeoutSec=10}
    return @{User=$login.user;Http=@{WebSession=$session;Headers=@{'X-CSRF-Token'=$login.csrf_token};TimeoutSec=10}}
}
function Assert-Status([int]$Expected, [string]$Path, [hashtable]$Http) {
    try { Invoke-RestMethod @Http -Uri "$base$Path" | Out-Null }
    catch {
        $response = $_.Exception.PSObject.Properties['Response']
        if ($null -ne $response -and $null -ne $response.Value -and [int]$response.Value.StatusCode -eq $Expected) { return }
        throw "Expected HTTP $Expected for $Path; $($_.Exception.Message)"
    }
    throw "Expected HTTP $Expected for $Path but request succeeded"
}

try {
    $adminCredential = @(Get-Content -LiteralPath $AdminCredentialsFile -Raw | ConvertFrom-Json)[0]
    $admin = Sign-In $adminCredential.username $adminCredential.password
    $adminHttp = $admin.Http
    $username = 'signup.' + [guid]::NewGuid().ToString('N')
    $password = [guid]::NewGuid().ToString('N') + '!Aa9'
    $input = @{username=$username;name='Fictional signup smoke user';password=$password}
    $signup = Post-Json '/api/auth/signup' $input @{TimeoutSec=10}
    $createdId = $signup.user.id
    Assert-Check ($signup.user.role -ceq 'employee' -and $createdId -ne '') 'Signup did not create an employee'
    Assert-Status 409 '/api/auth/signup' @{Method='Post';ContentType='application/json';Body=($input|ConvertTo-Json);TimeoutSec=10}
    $forged = $input + @{role='admin'}
    Assert-Status 400 '/api/auth/signup' @{Method='Post';ContentType='application/json';Body=($forged|ConvertTo-Json);TimeoutSec=10}
    $employee = Sign-In $username $password
    $employeeHttp = $employee.Http
    Assert-Status 401 '/api/admin/users' @{TimeoutSec=10}
    Assert-Status 403 '/api/admin/users' $employeeHttp
    $rolePath = "/api/admin/users/$createdId/role"
    Assert-Status 403 $rolePath ($employeeHttp + @{Method='Post';ContentType='application/json';Body='{"role":"admin"}'})

    $users = Invoke-RestMethod @adminHttp -Uri "$base/api/admin/users"
    Assert-Check (@($users.users | Where-Object id -eq $createdId).Count -eq 1) 'Admin user list did not contain signup'
    if (@($users.users | Where-Object role -eq 'admin').Count -eq 1) {
        Assert-Status 409 "/api/admin/users/$($admin.User.id)/role" ($adminHttp + @{Method='Post';ContentType='application/json';Body='{"role":"employee"}'})
    }
    $own = Post-Json '/api/purchases' @{client_request_id=[guid]::NewGuid().ToString();employee_id=$createdId;item_name='Fictional role-check own request';quantity=1;unit_price_cents=100} $employeeHttp
    $changed = Post-Json $rolePath @{role='manager'} $adminHttp
    Assert-Check ($changed.user.role -ceq 'manager') 'Admin role assignment failed'
    Assert-Status 401 '/api/auth/me' $employeeHttp

    if ($RestartGateway) {
        docker compose restart api-gateway
        if ($LASTEXITCODE -ne 0) { throw 'Gateway restart failed' }
        $ready = $false
        for ($i=0; $i -lt 20; $i++) {
            try { $health=Invoke-RestMethod -Uri "$base/healthz" -TimeoutSec 2; if ($health.status -eq 'ok') { $ready=$true; break } } catch {}
            Start-Sleep -Milliseconds 500
        }
        Assert-Check $ready 'Gateway did not restart'
        $admin = Sign-In $adminCredential.username $adminCredential.password
        $adminHttp = $admin.Http
    }
    $manager = Sign-In $username $password
    $managerHttp = $manager.Http
    Assert-Check ($manager.User.id -ceq $createdId -and $manager.User.role -ceq 'manager') 'Signup/password/role did not persist'
    Assert-Status 403 '/api/admin/users' $managerHttp
    Assert-Status 403 "/api/purchases/$($own.id)/decision" ($managerHttp + @{Method='Post';ContentType='application/json';Body='{"decision":"APPROVED"}'})
    $other = Post-Json '/api/purchases' @{client_request_id=[guid]::NewGuid().ToString();employee_id=$admin.User.id;item_name='Fictional delegated-manager check';quantity=2;unit_price_cents=2500} $adminHttp
    $approved = Post-Json "/api/purchases/$($other.id)/decision" @{decision='APPROVED'} $managerHttp
    Assert-Check ($approved.status -ceq 'APPROVED') 'Assigned manager could not approve another user request'

    Post-Json $rolePath @{role='admin'} $adminHttp | Out-Null
    Assert-Status 401 '/api/auth/me' $managerHttp
    $newAdmin = Sign-In $username $password
    $newAdminHttp = $newAdmin.Http
    Invoke-RestMethod @newAdminHttp -Uri "$base/api/admin/users" | Out-Null
    Post-Json $rolePath @{role='employee'} $newAdminHttp | Out-Null
    Assert-Status 401 '/api/admin/users' $newAdminHttp
    $final = Sign-In $username $password
    $finalHttp = $final.Http
    Assert-Check ($final.User.role -ceq 'employee') 'Demotion did not take effect'
    Assert-Status 403 '/api/admin/users' $finalHttp
    Post-Json '/api/auth/logout' @{} $finalHttp | Out-Null
    Post-Json '/api/auth/logout' @{} $adminHttp | Out-Null
    Write-Host "PASS: $username signup, admin role grants, session revocation, self-review protection, and real delegated approval. Synthetic account remains an employee."
    exit 0
} catch {
    Write-Error "Authentication smoke check failed: $($_.Exception.Message)" -ErrorAction Continue
    exit 1
} finally {
    # Restore only the synthetic account's role if a check failed after promotion.
    if ($createdId) {
        try {
            $cleanupAdmin = Sign-In $adminCredential.username $adminCredential.password
            $cleanupHttp = $cleanupAdmin.Http
            Post-Json "/api/admin/users/$createdId/role" @{role='employee'} $cleanupHttp | Out-Null
            Post-Json '/api/auth/logout' @{} $cleanupHttp | Out-Null
        } catch { Write-Warning 'Could not restore the synthetic account role; inspect Manage users.' }
    }
}

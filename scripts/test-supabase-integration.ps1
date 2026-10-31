$ErrorActionPreference = "Stop"

$repositoryRoot = Split-Path -Parent $PSScriptRoot
$environmentFile = Join-Path $repositoryRoot ".env"
if (-not (Test-Path -LiteralPath $environmentFile)) {
    throw "Create the ignored root .env file and set DATABASE_URL before running the schema check."
}

$databaseLine = Get-Content -LiteralPath $environmentFile |
    Where-Object { $_ -match '^\s*DATABASE_URL\s*=' } |
    Select-Object -Last 1
if (-not $databaseLine) {
    throw "Set DATABASE_URL in the ignored root .env file before running the schema check."
}

$databaseUrl = ($databaseLine -replace '^\s*DATABASE_URL\s*=\s*', '').Trim().Trim('"').Trim("'")
if ($databaseUrl -notmatch '^postgres(ql)?://\S+$') {
    throw "DATABASE_URL must be a PostgreSQL URL."
}

Push-Location (Join-Path $repositoryRoot "apps\api")
try {
    $env:TEST_DATABASE_URL = $databaseUrl
    go test ./... -count=1
    if ($LASTEXITCODE -ne 0) {
        throw "PostgreSQL integration checks failed with exit code $LASTEXITCODE."
    }
}
finally {
    Remove-Item Env:TEST_DATABASE_URL -ErrorAction SilentlyContinue
    Pop-Location
}

$ErrorActionPreference = "Stop"

$repositoryRoot = Split-Path -Parent $PSScriptRoot
$environmentFile = Join-Path $repositoryRoot ".env"
if (-not (Test-Path -LiteralPath $environmentFile)) {
    throw "Create the ignored root .env file and set DATABASE_URL before applying migrations."
}

$databaseLine = Get-Content -LiteralPath $environmentFile |
    Where-Object { $_ -match '^\s*DATABASE_URL\s*=' } |
    Select-Object -Last 1
if (-not $databaseLine) {
    throw "Set DATABASE_URL in the ignored root .env file before applying migrations."
}

$databaseUrl = ($databaseLine -replace '^\s*DATABASE_URL\s*=\s*', '').Trim().Trim('"').Trim("'")
if ($databaseUrl -notmatch '^postgres(ql)?://\S+$') {
    throw "DATABASE_URL must be a PostgreSQL URL."
}

Push-Location $repositoryRoot
try {
    & npm.cmd exec --yes --package=supabase@2.122.0 -- supabase db push --db-url $databaseUrl
    if ($LASTEXITCODE -ne 0) {
        throw "Supabase migration push failed with exit code $LASTEXITCODE."
    }
}
finally {
    Pop-Location
}

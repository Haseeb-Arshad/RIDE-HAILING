param([switch]$DryRun)

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
	$null = & npm.cmd exec --yes --package=supabase@2.122.0 -- supabase --version
    if ($LASTEXITCODE -ne 0) {
		throw "Could not prepare the Supabase CLI (exit code $LASTEXITCODE)."
	}

	$npmCache = (& npm.cmd config get cache).Trim()
	if ($LASTEXITCODE -ne 0) {
		throw "Could not locate the npm package cache (exit code $LASTEXITCODE)."
	}
	$cliBinary = Get-ChildItem -LiteralPath (Join-Path $npmCache "_npx") -Filter "supabase.exe" -File -Recurse -ErrorAction SilentlyContinue |
		Where-Object {
			$packageDirectory = Split-Path -Parent (Split-Path -Parent $_.FullName)
			$manifestPath = Join-Path $packageDirectory "package.json"
			if (-not (Test-Path -LiteralPath $manifestPath)) { return $false }
			$manifest = Get-Content -LiteralPath $manifestPath -Raw | ConvertFrom-Json
			$manifest.version -eq "2.122.0"
		} |
		Select-Object -First 1 -ExpandProperty FullName
	if (-not $cliBinary) {
		throw "The pinned Supabase CLI executable was not found in the npm cache."
	}

	$supabaseArgs = @("db", "push", "--db-url", $databaseUrl)
	if ($DryRun) { $supabaseArgs += "--dry-run" }
	& $cliBinary @supabaseArgs
	if ($LASTEXITCODE -ne 0) {
		throw "Supabase migration push failed with exit code $LASTEXITCODE."
    }
}
finally {
    Pop-Location
}

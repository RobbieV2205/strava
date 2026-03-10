# Strava OAuth2 authentication
# -----------------------------------------------
# Opens the browser to authenticate and get the right tokens needed for the application.
# Tokens will be stored in a file called: strava_tokens.json.
#
# If the tokens are collected successfully this script is no longer needed.
#
# Requirements: PowerShell 5.1+ or PowerShell Core (Windows / macOS / Linux)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

$ScriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$RootDir   = Split-Path -Parent $ScriptDir
$EnvFile   = Join-Path $RootDir ".env"

# --- Parse .env file ---
if (-not (Test-Path $EnvFile)) {
    Write-Error "ERROR: .env file not found at $EnvFile"
    exit 1
}

$Env = @{}
foreach ($line in Get-Content $EnvFile) {
    # Skip empty lines and full-line comments
    if ($line -match '^\s*$' -or $line -match '^\s*#') { continue }
    $parts = $line -split '=', 2
    if ($parts.Count -lt 2) { continue }
    $key   = $parts[0].Trim()
    $value = $parts[1] -replace '\s+#.*$', ''  # Remove inline comments
    $value = $value.Trim()
    $Env[$key] = $value
}

# --- Config ---
$ClientId     = $Env["STRAVA_CLIENT_ID"]
$ClientSecret = $Env["STRAVA_CLIENT_SECRET"]
$RedirectUri  = if ($Env["STRAVA_REDIRECT_URI"]) { $Env["STRAVA_REDIRECT_URI"] } else { "http://localhost:8080/callback" }
$TokenFile    = if ($Env["STRAVA_TOKEN_FILE"])    { $Env["STRAVA_TOKEN_FILE"] }    else { "strava_tokens.json" }

if (-not $ClientId -or -not $ClientSecret) {
    Write-Error "ERROR: STRAVA_CLIENT_ID and STRAVA_CLIENT_SECRET missing in .env"
    exit 1
}

# Extract port from redirect URI
$Port = ([System.Uri]$RedirectUri).Port
if ($Port -le 0) { $Port = 8080 }

$AuthUrl  = "https://www.strava.com/oauth/authorize"
$TokenUrl = "https://www.strava.com/oauth/token"

# Build authorization URL
$EncodedRedirect = [System.Uri]::EscapeDataString($RedirectUri)
$FullUrl = "$AuthUrl?client_id=$ClientId&redirect_uri=$EncodedRedirect&response_type=code&approval_prompt=auto&scope=read,activity:read_all"

# --- Start local HTTP callback server ---
$Listener = [System.Net.HttpListener]::new()
$Listener.Prefixes.Add("http://localhost:$Port/")

try {
    $Listener.Start()
} catch {
    Write-Error "ERROR: Could not start HTTP listener on port $Port. Is the port already in use?"
    exit 1
}

Write-Host "[auth] Browser opens for Strava authentication..."
Write-Host "[auth] Does the browser not work? Go to:`n  $FullUrl`n"

# Open browser
Start-Process $FullUrl

# --- Wait for callback (up to 120 seconds) ---
$ContextTask = $Listener.GetContextAsync()
$Timeout     = [System.TimeSpan]::FromSeconds(120)
$Stopwatch   = [System.Diagnostics.Stopwatch]::StartNew()

while (-not $ContextTask.IsCompleted -and $Stopwatch.Elapsed -lt $Timeout) {
    Start-Sleep -Milliseconds 200
}

$Listener.Stop()

if (-not $ContextTask.IsCompleted) {
    Write-Error "ERROR: Authentication timeout — no code received."
    exit 1
}

$Context  = $ContextTask.Result
$AuthCode = $Context.Request.QueryString["code"]

# Send response to browser
if ($AuthCode) {
    $ResponseBody = "<h2>Authorised! You can close this tab.</h2>"
    $Context.Response.StatusCode = 200
} else {
    $ResponseBody = "<h2>Authorisation failed.</h2>"
    $Context.Response.StatusCode = 400
}
$Bytes = [System.Text.Encoding]::UTF8.GetBytes($ResponseBody)
$Context.Response.ContentLength64 = $Bytes.Length
$Context.Response.OutputStream.Write($Bytes, 0, $Bytes.Length)
$Context.Response.OutputStream.Close()

if (-not $AuthCode) {
    Write-Error "ERROR: Authorisation failed — no code in callback."
    exit 1
}

# --- Exchange code for access token ---
Write-Host "[auth] Exchanging code for tokens..."

$Body = @{
    client_id     = $ClientId
    client_secret = $ClientSecret
    code          = $AuthCode
    grant_type    = "authorization_code"
}

try {
    $Response = Invoke-RestMethod -Method Post -Uri $TokenUrl -Body $Body
} catch {
    Write-Error "ERROR: Token exchange failed: $_"
    exit 1
}

$Json = $Response | ConvertTo-Json -Depth 10
Set-Content -Path $TokenFile -Value $Json -Encoding UTF8

Write-Host "[auth] Token stored in: $TokenFile"

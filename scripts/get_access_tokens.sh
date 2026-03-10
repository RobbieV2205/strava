#!/usr/bin/env bash
# Strava OAuth2 authentication
# -----------------------------------------------
# Opens the browser to authenticate and get the right tokens needed for the application.
# Tokens will be stored in a file called: strava_tokens.json.
#
# If the tokens are collected successfully this script is no longer needed.
#
# Requirements: bash, curl, python3 (stdlib only — no pip packages needed)

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(dirname "$SCRIPT_DIR")"
ENV_FILE="$ROOT_DIR/.env"

# --- Parse .env file ---
if [[ ! -f "$ENV_FILE" ]]; then
    echo "ERROR: .env file not found at $ENV_FILE"
    exit 1
fi

parse_env() {
    local file="$1"
    while IFS= read -r line || [[ -n "$line" ]]; do
        # Skip empty lines and full-line comments
        [[ "$line" =~ ^[[:space:]]*$ ]] && continue
        [[ "$line" =~ ^[[:space:]]*# ]] && continue
        # Split on first '='
        local key="${line%%=*}"
        local value="${line#*=}"
        # Remove inline comments (space followed by #)
        value="${value%% #*}"
        # Trim surrounding whitespace
        key="${key#"${key%%[! ]*}"}"; key="${key%"${key##*[! ]}"}"
        value="${value#"${value%%[! ]*}"}"; value="${value%"${value##*[! ]}"}"
        [[ -n "$key" ]] && export "$key=$value"
    done < "$file"
}

parse_env "$ENV_FILE"

# --- Config ---
CLIENT_ID="${STRAVA_CLIENT_ID:-}"
CLIENT_SECRET="${STRAVA_CLIENT_SECRET:-}"
REDIRECT_URI="${STRAVA_REDIRECT_URI:-http://localhost:8080/callback}"
TOKEN_FILE="${STRAVA_TOKEN_FILE:-strava_tokens.json}"

if [[ -z "$CLIENT_ID" || -z "$CLIENT_SECRET" ]]; then
    echo "ERROR: STRAVA_CLIENT_ID and STRAVA_CLIENT_SECRET missing in .env"
    exit 1
fi

if ! command -v curl &>/dev/null; then
    echo "ERROR: curl is required but not installed."
    exit 1
fi

if ! command -v python3 &>/dev/null; then
    echo "ERROR: python3 is required for the local callback server."
    exit 1
fi

# Extract port from redirect URI
PORT=$(python3 -c "from urllib.parse import urlparse; print(urlparse('$REDIRECT_URI').port or 8080)")

AUTH_URL="https://www.strava.com/oauth/authorize"
TOKEN_URL="https://www.strava.com/oauth/token"

# URL-encode the redirect URI
ENCODED_REDIRECT=$(python3 -c "from urllib.parse import quote; print(quote('$REDIRECT_URI', safe=''))")
FULL_URL="${AUTH_URL}?client_id=${CLIENT_ID}&redirect_uri=${ENCODED_REDIRECT}&response_type=code&approval_prompt=auto&scope=read,activity:read_all"

# --- Start local HTTP callback server (Python3 stdlib only) ---
CODE_FILE="$(mktemp)"
trap 'rm -f "$CODE_FILE"' EXIT

python3 - "$CODE_FILE" "$PORT" <<'PYEOF' &
import http.server, urllib.parse, sys

code_file = sys.argv[1]
port      = int(sys.argv[2])

class Handler(http.server.BaseHTTPRequestHandler):
    def do_GET(self):
        params = urllib.parse.parse_qs(urllib.parse.urlparse(self.path).query)
        if "code" in params:
            open(code_file, "w").write(params["code"][0])
            self.send_response(200)
            self.end_headers()
            self.wfile.write(b"<h2>Authorised! You can close this tab.</h2>")
        else:
            self.send_response(400)
            self.end_headers()
            self.wfile.write(b"<h2>Authorisation failed.</h2>")
    def log_message(self, *a):
        pass

http.server.HTTPServer(("localhost", port), Handler).handle_request()
PYEOF
SERVER_PID=$!

echo "[auth] Browser opens for Strava authentication..."
echo "[auth] Does the browser not work? Go to:"
echo "  $FULL_URL"
echo ""

# Open browser (Linux / macOS)
if command -v xdg-open &>/dev/null; then
    xdg-open "$FULL_URL" &>/dev/null &
elif command -v open &>/dev/null; then
    open "$FULL_URL"
else
    echo "[auth] Could not detect a browser opener. Open the URL above manually."
fi

# --- Wait for auth code (up to 120 seconds) ---
AUTH_CODE=""
for _ in $(seq 1 120); do
    if [[ -s "$CODE_FILE" ]]; then
        AUTH_CODE="$(cat "$CODE_FILE")"
        break
    fi
    sleep 1
done

kill "$SERVER_PID" 2>/dev/null || true

if [[ -z "$AUTH_CODE" ]]; then
    echo "ERROR: Authentication timeout — no code received."
    exit 1
fi

# --- Exchange code for access token ---
echo "[auth] Exchanging code for tokens..."
RESPONSE=$(curl -s -X POST "$TOKEN_URL" \
    --data-urlencode "client_id=$CLIENT_ID" \
    --data-urlencode "client_secret=$CLIENT_SECRET" \
    --data-urlencode "code=$AUTH_CODE" \
    --data-urlencode "grant_type=authorization_code")

if python3 -c "import sys, json; d=json.loads(sys.argv[1]); sys.exit(0 if 'access_token' in d else 1)" "$RESPONSE" 2>/dev/null; then
    echo "$RESPONSE" | python3 -c "import sys, json; print(json.dumps(json.load(sys.stdin), indent=2))" > "$TOKEN_FILE"
    echo "[auth] Token stored in: $TOKEN_FILE"
else
    echo "ERROR: Token exchange failed: $RESPONSE"
    exit 1
fi

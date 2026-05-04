#!/usr/bin/env bash
# Quick smoke test for the Flux Lambda API.
# Confirms /status, /history, /day all return 200 and that /day's summary
# carries the new offpeakGridImportKwh / offpeakGridExportKwh fields.
#
# Usage:
#   FLUX_API_URL=https://… FLUX_API_TOKEN=… ./scripts/check-api.sh
#   ./scripts/check-api.sh https://… TOKEN

set -euo pipefail

URL="${1:-${FLUX_API_URL:-}}"
TOKEN="${2:-${FLUX_API_TOKEN:-}}"

if [[ -z "$URL" || -z "$TOKEN" ]]; then
    cat >&2 <<EOF
Missing URL or token.
  $0 <api-url> <token>
or set FLUX_API_URL and FLUX_API_TOKEN env vars.
EOF
    exit 2
fi

if ! command -v jq >/dev/null 2>&1; then
    echo "Error: jq is required. Install with 'brew install jq'." >&2
    exit 2
fi

URL="${URL%/}"
# All Flux date keys are Sydney-local; use that TZ so the script doesn't
# request tomorrow's date and false-fail the off-peak split check when
# run between Sydney midnight and UTC midnight.
TODAY="$(TZ=Australia/Sydney date +%Y-%m-%d)"

# All status text goes to stderr so command-substitution captures only the
# JSON body returned by `fetch`.
bold() { printf '\033[1m%s\033[0m\n' "$1" >&2; }
ok()   { printf '  \033[32m✓\033[0m %s\n' "$1" >&2; }
fail() { printf '  \033[31m✗\033[0m %s\n' "$1" >&2; }
indent() { sed 's/^/    /' >&2; }

# Fetches a path, prints body to stdout, status to stderr.
# Exits the script with code 1 if the request fails (status != 200).
fetch() {
    local label="$1" path="$2"
    bold "$label  GET $path"
    local body
    body="$(mktemp)"
    local code
    code=$(curl -sS -o "$body" -w '%{http_code}' \
        -H "Authorization: Bearer $TOKEN" \
        "$URL$path" || true)
    if [[ "$code" == "200" ]]; then
        ok "HTTP 200"
    else
        fail "HTTP ${code:-no response}"
        head -3 "$body" | indent
        rm -f "$body"
        exit 1
    fi
    cat "$body"
    rm -f "$body"
}

echo >&2
status_body=$(fetch "[1/3] /status" "/status")
echo "$status_body" \
    | jq '{soc: .live.soc, batteryMode: (if .live.pbat>0 then "discharging" elif .live.pbat<0 then "charging" else "idle" end), offpeakGridUsageKwh: .offpeak.gridUsageKwh, todayEpv: .todayEnergy.epv}' \
    | indent

echo >&2
history_body=$(fetch "[2/3] /history?days=7" "/history?days=7")
echo "$history_body" \
    | jq '{count: (.days | length), latest: (.days | last | {date, eInput, offpeakGridImportKwh})}' \
    | indent

echo >&2
day_body=$(fetch "[3/3] /day?date=$TODAY" "/day?date=$TODAY")
echo "$day_body" \
    | jq '.summary | {eInput, eOutput, offpeakGridImportKwh, offpeakGridExportKwh}' \
    | indent

echo >&2
bold "Summary checks"

has_field=$(echo "$day_body" | jq '.summary | has("offpeakGridImportKwh")')
import_value=$(echo "$day_body" | jq '.summary.offpeakGridImportKwh')

if [[ "$has_field" == "true" && "$import_value" != "null" ]]; then
    ok "/day summary has offpeakGridImportKwh = $import_value (new Lambda is live)"
elif [[ "$has_field" == "true" && "$import_value" == "null" ]]; then
    ok "/day summary key present but null (new Lambda is live; no off-peak record yet for $TODAY)"
else
    fail "/day summary missing offpeakGridImportKwh — Lambda hasn't been rebuilt with the new code"
    cat >&2 <<EOF
    Rebuild with:
      GOOS=linux GOARCH=arm64 go build -o lambda/bootstrap ./cmd/api
      aws cloudformation package --template-file infrastructure/template.yaml --s3-bucket BUCKET --output-template-file infrastructure/packaged.yaml
      aws cloudformation deploy --template-file infrastructure/packaged.yaml --stack-name flux --capabilities CAPABILITY_IAM ...
EOF
    exit 1
fi

echo >&2
bold "All checks passed."

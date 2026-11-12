#!/bin/sh
# Usage: verify-instance.sh <base-url> <site-name> [<interval-minutes>]
# Exit 0 when all checks pass, 1 after printing the names of failed checks.
# Without <interval-minutes> the prices-fresh check is skipped.
set -u

if [ "$#" -lt 2 ] || [ "$#" -gt 3 ]; then
    echo "usage: $0 <base-url> <site-name> [<interval-minutes>]" >&2
    exit 2
fi

base_url=${1%/}
site_name=$2
interval=${3:-}
failed=""

fail() {
    failed="$failed $1"
    echo "FAIL $1"
}

pass() {
    echo "ok   $1"
}

fetch_status() {
    curl -sL -o /dev/null -w '%{http_code}' --max-time 20 "$base_url$1" 2>/dev/null
}

fetch_body() {
    curl -sL --max-time 20 "$base_url$1" 2>/dev/null
}

if [ "$(fetch_status /robots.txt)" = "200" ]; then pass robots; else fail robots; fi
if [ "$(fetch_status /favicon.ico)" = "200" ]; then pass favicon; else fail favicon; fi

about=$(fetch_body /about)
if printf '%s' "$about" | grep -qF -- "$site_name"; then pass branding; else fail branding; fi
if [ -n "$about" ] && ! printf '%s' "$about" | grep -qF 'status.evepraisal.com'; then
    pass no-status-link
else
    fail no-status-link
fi

# humanize.Time renders "N seconds ago", "N minutes ago", "N hours ago", "N days ago", "now".
prices_fresh() {
    stamps=$(fetch_body /item/34 | grep -oE 'updated ([0-9]+ [a-z]+ ago|now)')
    [ -n "$stamps" ] || return 1
    limit=$((interval + 10))
    echo "$stamps" | while read -r _ count unit _; do
        case "$unit" in
            ago | "") ;;
            second | seconds) ;;
            minute | minutes) [ "$count" -le "$limit" ] || exit 1 ;;
            *) exit 1 ;;
        esac
    done
}

if [ -n "$interval" ]; then
    if prices_fresh; then pass prices-fresh; else fail prices-fresh; fi
fi

if [ -n "$failed" ]; then
    echo "failed:$failed"
    exit 1
fi

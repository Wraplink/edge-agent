#!/usr/bin/env bash

set -euo pipefail

PROJECT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DLV="${DLV:-$HOME/go/bin/dlv}"
BINARY="$PROJECT_DIR/build/edge-agent"
CONFIG="$PROJECT_DIR/config.yaml"

cd "$PROJECT_DIR"

# Support both local environment file naming conventions.
if [ -f "$PROJECT_DIR/.env.local" ]; then
    ENV_FILE="$PROJECT_DIR/.env.local"
else
    echo "ERROR: create .env.local in $PROJECT_DIR"
    exit 1
fi

# Sourcing alone does not export shell variables to child processes.
set -a
# shellcheck disable=SC1090
source "$ENV_FILE"
set +a

if [ -z "${DNS_CONTROL_API_URL:-}" ] || [ -z "${DNS_CONTROL_API_TOKEN:-}" ]; then
    echo "ERROR: $ENV_FILE must define DNS_CONTROL_API_URL and DNS_CONTROL_API_TOKEN"
    exit 1
fi

if [ ! -x "$DLV" ]; then
    echo "ERROR: Delve not found: $DLV"
    exit 1
fi

mkdir -p "$PROJECT_DIR/build"
go build -gcflags="all=-N -l" -o "$BINARY" ./cmd/edge-agent

echo "========================================"
echo " WrapLink edge-agent debugger"
echo "========================================"
echo "Config:     $CONFIG"
echo "Env file:   $ENV_FILE"
echo "Binary:     $BINARY"
echo "Delve:      $DLV"
echo "Debug port: 127.0.0.1:2345"
echo "Attach GoLand; execution begins when the debugger continues."

# Keep the two variables through sudo (sudo normally clears most environment).
# Never echo the API token.
exec sudo --preserve-env=DNS_CONTROL_API_URL,DNS_CONTROL_API_TOKEN "$DLV" exec \
    "$BINARY" \
    --headless \
    --listen=127.0.0.1:2345 \
    --api-version=2 \
    --accept-multiclient \
    --only-same-user=false \
    -- -config "$CONFIG"

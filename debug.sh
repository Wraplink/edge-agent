#!/usr/bin/env bash

set -e

PROJECT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DLV="/home/asgarinia/go/bin/dlv"
BINARY="$PROJECT_DIR/build/edge-agent"

echo "PROJECT_DIR=$PROJECT_DIR"
echo "DLV=$DLV"
echo "BINARY=$BINARY"

if [ ! -x "$DLV" ]; then
    echo "ERROR: Delve not found: $DLV"
    exit 1
fi

if [ ! -x "$BINARY" ]; then
    echo "ERROR: edge-agent binary not found: $BINARY"
    exit 1
fi

echo
echo "========================================"
echo " WrapLink edge-agent debugger"
echo "========================================"
echo
echo "Binary:     $BINARY"
echo "Delve:      $DLV"
echo "Debug port: 127.0.0.1:2345"
echo
echo "Attach GoLand now."
echo "Program is paused until debugger continues."
echo

go build -o build/edge-agent ./cmd/edge-agent

source .env.local

exec sudo "$DLV" exec \
    "$BINARY" \
    --headless \
    --listen=127.0.0.1:2345 \
    --api-version=2 \
    --accept-multiclient \
    --only-same-user=false
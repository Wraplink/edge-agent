#!/usr/bin/env bash
set -euo pipefail

# Run as root on a disposable Linux test host; no host firewall tables are modified.
command -v unshare >/dev/null || { echo "unshare is required" >&2; exit 1; }
command -v ip >/dev/null || { echo "iproute2 is required" >&2; exit 1; }
command -v nft >/dev/null || { echo "nft is required" >&2; exit 1; }
command -v go >/dev/null || { echo "go is required" >&2; exit 1; }

cd "$(dirname "$0")/.."
exec unshare --net --fork -- bash -c '
  set -euo pipefail
  ip link set lo up
  export WRAPLINK_KERNEL_TEST=1
  go test -count=1 -run "^TestKernel(EntitlementExpiresWithoutRefresh|RestartRevokesStaleEntitlements|EstablishedUDPRevocation)$" -v ./internal/dataplane
'

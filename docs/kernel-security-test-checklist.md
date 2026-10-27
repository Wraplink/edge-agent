# Privileged kernel security test checklist

The normal Go CI pipeline compiles the integration tests but skips their kernel
execution. A green CI run is **not** evidence that nftables authorization works.

## Prerequisites

Use a disposable Linux VM with root privileges, `go`, `iproute2`,
`util-linux` (`unshare`), `nftables`, and kernel support for network
namespaces, veth, conntrack and nftables sets with timeouts.

Run from the repository root:

```bash
sudo bash scripts/test-kernel-entitlements.sh
```

The runner creates a separate network and mount namespace, then the PREROUTING
test creates a peer namespace and veth pair inside that isolated environment.
Do not run the Go tests directly with `WRAPLINK_KERNEL_TEST=1` on a production
host: they create and modify a table named `wraplink`.

## Required evidence

Record the kernel version, nftables version, command exit status, and complete
Go test output. All of the following must pass without skips:

- `TestKernelEntitlementExpiresWithoutRefresh`
- `TestKernelRestartRevokesStaleEntitlements`
- `TestKernelEstablishedUDPRevocation`
- `TestKernelEstablishedTCPRevocation`
- `TestKernelPreroutingUDPRevocation`

The PREROUTING UDP test currently creates a new socket for each send; it proves
packet authorization after revocation but **does not** prove revocation of an
established remote UDP flow. The OUTPUT UDP and TCP tests reuse sockets, but
local loopback behavior may differ from traffic arriving on a physical NIC.

## Production gate

Do not close issue #32 based on green standard CI alone. Still required:
a persistent remote UDP and TCP flow across revocation; verification of
conntrack behavior; restart behavior during the interval before entitlement
reconciliation; and negative tests for unauthorized sources. Any failure must
be treated as a release blocker for Full Route authorization.

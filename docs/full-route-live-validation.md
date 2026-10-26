# Full Route authorization: live validation

This runbook is for a **disposable test environment**, not a production gateway. PR #29 added nftables source entitlement guards and PR #30 tests their rule structure. Neither proves kernel-level behavior.

## Preconditions

- Run as root on a dedicated Debian/Linux test host with nftables and conntrack installed.
- Deploy the current Edge Agent build; enable Full Route source enforcement.
- Have a separate authorized test client and a separate unauthorized test client. Do **not** test remote PREROUTING exclusively from the Edge Agent host: local traffic takes OUTPUT instead.
- Use a test-only allocated IPv4 address and test origin service. Verify both can be safely accessed. Do not reuse a production destination.
- Record the exact lease ID, edge IP, origin IP, client IPs, and the local OUTPUT setting.
- Keep an out-of-band management session open. Never flush production firewall tables or the entitlement set to simulate revocation.

## Read-only inspection

```sh
sudo nft -a list table ip wraplink
sudo nft -a list chain ip wraplink guard_prerouting
sudo nft -a list chain ip wraplink guard_output
sudo nft list set ip wraplink entitled_v4
sudo conntrack -L -f ipv4 2>/dev/null
```

Expected: guard_prerouting is a filter chain on prerouting at raw priority, before conntrack and DNAT. When local OUTPUT routing is enabled, guard_output is also present. Each protected destination has a rule matching its allocated IPv4 address, followed by an inverted lookup of the source in entitled_v4, followed by drop. DNAT rules are in the separate NAT chain.

## Test matrix

| Scenario | Action | Required observation |
| --- | --- | --- |
| No grant | From unauthorized client, initiate TCP and UDP traffic to allocated IP | Packets are dropped; origin sees no traffic |
| Grant added | Grant test client access via the normal control plane; inspect entitled_v4 | Client IP appears and new TCP/UDP flows work |
| Wrong client | From a second client with no grant, retry | Still dropped |
| Grant revoked | Disconnect/revoke through the normal control plane; inspect entitled_v4 | Client IP disappears; new flows blocked |
| Existing TCP flow | Establish a long-running authorized flow; revoke while it is active | New packets must stop reaching origin; document any surviving flow |
| Existing UDP flow | Send repeated datagrams while authorized; revoke | Subsequent datagrams must stop reaching origin |
| Agent restart | Restart Edge Agent while route remains configured | No period of unauthorized access, including during startup/reconciliation |
| Agent unavailable | Stop agent/control plane in isolated test environment | Define and verify expected fail-closed behavior |
| Reassignment | Change lease to a different origin/lease ID for same allocated IP | No stale NAT or guard rule permits unauthorized traffic |

For each scenario, capture source/destination IPs, UTC timestamp, nftables counters or trace, packet captures at origin, and connection state. A successful TCP connect alone is insufficient evidence of source authorization.

## Capture evidence

```sh
sudo nft -a list table ip wraplink
sudo conntrack -L -f ipv4 2>/dev/null
sudo nft monitor trace
```

Tracing requires explicitly configured nftrace rules. Only enable temporary tracing on a disposable test host, and remove it afterward. Prefer narrowly scoped packet captures at the test origin rather than broad production captures.

## Release gate

Do not claim production-ready authorization until all matrix cases are reproduced, including restart and established-session revocation. A failing test should produce a dedicated fix PR with a regression test. Document kernel and nftables versions and whether the test was remote PREROUTING or local OUTPUT.

## Known design questions

- An nftables set contains IPv4 addresses, not grant expiration timestamps. Verify that the agent/control plane refreshes it promptly on expiry and disconnect.
- A rule matched against the original destination before DNAT may not cover all reply-direction packets. Validate actual bidirectional flow behavior.
- Reconciliation currently removes and reinstalls some lease rules in separate flush operations. Verify restart and replacement fail-closed properties under real traffic.
- Old or orphaned lease rules must not bypass source checks.

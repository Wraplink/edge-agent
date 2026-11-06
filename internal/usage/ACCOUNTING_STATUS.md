# Full Route accounting status

This branch is an implementation foundation, not production billing support.

- The IPv4 FORWARD accounting chain is created by the dataplane.
- Route counter rendering and reading are not wired into the agent runtime.
- The rule installer refuses to flush an occupied chain because that would discard unreported bytes.
- Sampler checkpoints and outbox reports are not committed in one crash-safe transaction.
- The installer currently requires an external nft executable and must be validated against the deployed image.
- Destination port scoping, live TCP/UDP integration tests, and crash recovery remain outstanding.

**Do not enable this feature for billing or merge as production-ready until the above gaps are resolved.**

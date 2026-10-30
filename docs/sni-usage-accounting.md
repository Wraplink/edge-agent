# SNI traffic usage measurement (work in progress)

The SNI proxy can emit a `SessionUsage` callback when an upstream
connection terminates. `UploadedBytes` counts TLS ClientHello bytes
successfully written to the upstream plus subsequent client-to-upstream
stream bytes; `DownloadedBytes` counts bytes successfully copied from
upstream to client. Values exclude IP/TCP headers, retransmissions, and
connections rejected before an upstream connection is established.

**Not production billing ready.** The callback is synchronous and is not
connected to the backend. Callers must persist events durably before
acknowledging them, provide a stable session identifier, and ensure
retries cannot double-charge. A process crash can currently lose the
measurement for an active session. Session completion alone also does
not support timely enforcement for long-lived streams.

Full Route traffic needs independent nftables measurements. Ensure
packets are not counted again by the SNI meter when combining sources.

See edge-agent issue #46 and backend issue #62 for the complete
end-to-end accounting and quota enforcement requirements.

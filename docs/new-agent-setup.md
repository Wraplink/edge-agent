# Deploy a new WrapLink Edge Agent (Debian 13)

This procedure deploys **only** the edge-agent, not the central WrapLink platform. Run commands on the new edge host unless noted.

## 1. Prerequisites

- Debian 13, Docker Engine and Docker Compose plugin, Git, outbound HTTPS to central control APIs.
- Dedicated IPv4 assigned to the host, and inbound TCP/443 forwarded to this host for SNI traffic.
- Central Nginx HTTPS vhosts `https://api.wraplink.ir` and `https://dns-api.wraplink.ir` with valid certificates.
- The edge host's **actual outbound IPv4** must be allowed by the central Nginx API allowlist. Determine it using `curl --noproxy '*' -4fsS https://api.ipify.org`.
- Obtain the correct DNS Control agent token and Backend network control key **through a secure channel**. Never commit them.

On the edge host, test `curl --noproxy '*' -Iv https://api.wraplink.ir` and `curl --noproxy '*' -Iv https://dns-api.wraplink.ir`. HTTP 401/404 can be normal without credentials; TLS validation must pass. A 403 suggests IP allowlisting.

## 2. Check out code

```bash
git clone https://github.com/Wraplink/edge-agent.git /opt/wraplink/edge-agent
cd /opt/wraplink/edge-agent
```

Use a reviewed release or merged branch containing environment-driven config. Do not run the central platform's Compose file.

## 3. Configure this agent

```bash
cp .env.example .env.local
chmod 600 .env.local
nano .env.local
```

Example (replace values, never share credentials):

```dotenv
EDGE_AGENT_ID=edge-eu-02
EDGE_AGENT_REGION=eu-1
EDGE_PUBLIC_IP=185.194.190.84
EDGE_SNI_CAPACITY=5000
EDGE_ROUTE_CAPACITY=0
EDGE_SNI_ENABLED=true
EDGE_SNI_LISTEN_ADDRESS=0.0.0.0:443
DNS_CONTROL_API_URL=https://dns-api.wraplink.ir
DNS_CONTROL_API_TOKEN=<secret>
WRAPLINK_BACKEND_URL=https://api.wraplink.ir
NETWORK_CONTROL_KEY=<secret>
```

`config.yml` references these values with `${VARIABLE}`; the Go config loader expands them at runtime. Compose `env_file` passes `.env.local` into the container. Missing variables cause configuration failure. Set `edge.addresses` only to IPs assigned to this host; do not duplicate addresses.

Initially use `EDGE_ROUTE_CAPACITY=0` and `backend.route_public_ips: []`. Full Route requires dedicated public IPv4 addresses, corresponding route inventory in Backend, and reviewed nftables forwarding rules; do not enable it on a single SNI IP without planning.

## 4. Start

```bash
docker compose -f docker-compose.yml config --quiet
docker compose -f docker-compose.yml up -d --build
docker compose -f docker-compose.yml ps
docker compose -f docker-compose.yml logs --tail=100 -f edge-agent
```

This container uses host networking and `NET_ADMIN`/`NET_RAW` privileges. Verify that port 443 is free first (`ss -lntp | grep ':443 '`). The container runs as UID 10001:10001; confirm required host networking capabilities and nftables access are actually available before enabling routing.

## 5. Verify

- Logs should show successful registration and periodic heartbeats to the control plane.
- On the central Backend/DNS Control admin diagnostics, confirm the expected agent ID, observed IP, advertised IPs, SNI status, last seen, and capacity.
- Verify TCP/443 from an external client and test a managed SNI domain end-to-end. Do not infer success solely from the container being up.
- Check `docker compose logs --tail=200 edge-agent` for TLS, authentication, port binding, and permission errors.

## 6. Troubleshooting

- TLS/connection refused: test from the edge host; the central server may not support hairpin NAT to its own public IP.
- HTTP 403: check edge **egress** IP against Nginx allowlist.
- HTTP 401: verify agent token/control key, without printing secrets.
- Port 443 conflict: `ss -lntp | grep ':443 '`.
- Missing `${VARIABLE}`: update `.env.local`; do not disable strict config validation.
- After changing `.env.local`, recreate the container with `docker compose up -d --force-recreate`.

Do not expose the diagnostics API publicly. See the central platform's `docs/control-api-https.md` for certificate and allowlist setup.

## Backend agent-health registration

The Edge Agent now registers and heartbeats directly with Backend using `backend.url` (`WRAPLINK_BACKEND_URL`) and `backend.control_key` (`NETWORK_CONTROL_KEY`). Configure both even for SNI-only agents before enabling Backend-authoritative DNS snapshots. The key must match Backend `NETWORK_CONTROL_SYNC_KEY`; no separate agent-health secret is required. Check agent logs for `Backend health synchronization failed` and verify `network_agent_health` in Backend PostgreSQL. For Full Route, both the route-IP inventory heartbeat and agent-health heartbeat must remain fresh. See [coordinated rollout](https://github.com/Wraplink/backend/blob/master/docs/agent-registry-rollout.md).

## Optional private diagnostics API

Diagnostics are disabled by default and are **not required** for Backend agent registration or heartbeat reporting. To enable them, generate a random secret (`openssl rand -hex 32`), add `WRAPLINK_DIAGNOSTICS_TOKEN=<secret>` to the untracked `.env.local`, and update `config.yml`:

```yaml
diagnostics:
  enabled: true
  listen_address: "127.0.0.1:9091"
  token: "${WRAPLINK_DIAGNOSTICS_TOKEN}"
```

The shipped configuration uses an empty literal token and `enabled: false`; merely adding the environment variable does not enable diagnostics. Bind to loopback and access via SSH tunnel when needed; never publish this endpoint to the internet. Recreate the container after changing its configuration.

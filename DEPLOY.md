# Deploying PokéArena

The whole platform is one Docker image (five binaries) plus three
infrastructure dependencies — PostgreSQL, Redis, RabbitMQ. The supported
deployment path is the same `docker-compose.yml` this repo is developed and
tested against — same artifact, same command, zero translation.

A cloud-PaaS walkthrough (Railway-style: each binary as its own managed
service, managed Postgres + Redis, RabbitMQ from the official image) was
removed from this doc while we revisit hosting choices. The current
docker-compose setup runs equivalently on any VM that has Docker.

---

## Single VM (recommended)

Any box with Docker — a DigitalOcean droplet, a Hetzner VM, AWS Lightsail.

```bash
git clone <this-repo> pokearena && cd pokearena
cp .env.example .env
docker compose up -d --build
```

That starts Postgres, Redis, RabbitMQ, runs `ingest` once, and starts the four
services. Open `http://<vm-ip>:8080`.

To put it behind a domain with TLS, run a reverse proxy (Caddy is one line:
`reverse_proxy localhost:8080`) — WebSocket upgrades pass through automatically.

The same `docker compose up` that a developer runs locally is what runs on the
server: "works on my machine" and "works on the server" are the *same command*.

### Notes for any cloud-PaaS deployment

If/when we re-add a PaaS path, the contract is unchanged: each binary is its
own deployable, all five (`gateway`, `battle-worker`, `ai-service`,
`leaderboard-worker`, `ingest`) build from the same `Dockerfile` with different
start commands. Postgres and Redis can be managed services; RabbitMQ runs from
`rabbitmq:3-management-alpine`. The gateway is the only public endpoint;
workers and the AI service don't need ingress.

No LLM credentials are required by any cloud service in this deployment.
LLM play lives client-side of the gateway WS — through `pokearena-mcp`
(for MCP clients like Claude Code) or the reference harness
`cmd/pokearena-agent`, both of which run on the user's machine and hold
their own keys. See [`docs/agent-harness.md`](docs/agent-harness.md) for
the boundary.

---

## Configuration reference

| Variable | Purpose | Default |
|---|---|---|
| `DATABASE_URL` | PostgreSQL DSN | local compose value |
| `REDIS_URL` | Redis URL | local compose value |
| `RABBITMQ_URL` | AMQP URL | local compose value |
| `PORT` / `GATEWAY_ADDR` | gateway listen port | `:8080` |
| `DATA_VERSION` | dataset + cache namespace | `gen1-v1` |
| `AI_DIFFICULTY` | `easy` \| `hard` | `hard` |
| `AI_TIME_BUDGET_MS` | per-decision AI budget | `1500` |
